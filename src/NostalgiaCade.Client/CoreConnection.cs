using System.Buffers.Binary;
using System.IO.Pipes;
using System.Text.Json;

namespace NostalgiaCade.Client;

/// <summary>One ordered request stream. A failed/timed-out exchange closes the
/// stream so a late response can never be consumed by another request.</summary>
public sealed class CoreConnection : IAsyncDisposable
{
    public const int ProtocolVersion = 1;
    public const int MaxFrameBytes = 1024 * 1024;
    private static readonly JsonSerializerOptions Json = new(JsonSerializerDefaults.Web);
    private readonly NamedPipeClientStream _pipe;
    private readonly SemaphoreSlim _gate = new(1, 1);
    private long _nextId;

    private CoreConnection(NamedPipeClientStream pipe) => _pipe = pipe;

    public static async Task<CoreConnection> ConnectAsync(string pipeName, CancellationToken cancellationToken)
    {
        if (string.IsNullOrWhiteSpace(pipeName) || !pipeName.StartsWith("NostalgiaCade.", StringComparison.Ordinal) ||
            pipeName.Length > 100 || pipeName.Any(c => !char.IsAsciiLetterOrDigit(c) && c != '.'))
            throw new InvalidDataException("Invalid local core endpoint.");

        var pipe = new NamedPipeClientStream(".", pipeName, PipeDirection.InOut,
            PipeOptions.Asynchronous | PipeOptions.CurrentUserOnly);
        var connection = new CoreConnection(pipe);
        try
        {
            await pipe.ConnectAsync(1500, cancellationToken);
            var hello = await connection.CallAsync<CoreHello>("system.hello",
                new { protocolVersion = ProtocolVersion }, cancellationToken);
            if (hello.ProtocolVersion != ProtocolVersion)
                throw new InvalidDataException("The desktop and core protocol versions do not match.");
            return connection;
        }
        catch { await connection.DisposeAsync(); throw; }
    }

    public async Task<T> CallAsync<T>(string method, object? parameters = null, CancellationToken cancellationToken = default)
    {
        using var deadline = CancellationTokenSource.CreateLinkedTokenSource(cancellationToken);
        deadline.CancelAfter(TimeSpan.FromSeconds(5));
        await _gate.WaitAsync(deadline.Token);
        try
        {
            var id = (++_nextId).ToString(System.Globalization.CultureInfo.InvariantCulture);
            var bytes = JsonSerializer.SerializeToUtf8Bytes(new { jsonrpc = "2.0", id, method, @params = parameters }, Json);
            await WriteFrameAsync(_pipe, bytes, deadline.Token);
            using var response = JsonDocument.Parse(await ReadFrameAsync(_pipe, deadline.Token));
            var root = response.RootElement;
            if (root.ValueKind != JsonValueKind.Object ||
                !root.TryGetProperty("jsonrpc", out var version) || version.ValueKind != JsonValueKind.String || version.GetString() != "2.0" ||
                !root.TryGetProperty("id", out var responseId) || responseId.ValueKind != JsonValueKind.String || responseId.GetString() != id)
                throw new InvalidDataException("Unexpected response from the local core.");
            if (root.TryGetProperty("error", out var error))
            {
                if (root.TryGetProperty("result", out _) || error.ValueKind != JsonValueKind.Object ||
                    !error.TryGetProperty("code", out var code) || code.ValueKind != JsonValueKind.Number || !code.TryGetInt32(out var errorCode) ||
                    !error.TryGetProperty("message", out var message) || message.ValueKind != JsonValueKind.String)
                    throw new InvalidDataException("Invalid error response from the local core.");
                throw new CoreRpcException(errorCode, message.GetString()!);
            }
            if (!root.TryGetProperty("result", out var result))
                throw new InvalidDataException("The core response is missing a result.");
            return result.Deserialize<T>(Json)
                ?? throw new InvalidDataException("The core returned an empty result.");
        }
        catch (CoreRpcException) { throw; }
        catch { await _pipe.DisposeAsync(); throw; }
        finally { _gate.Release(); }
    }

    public static async Task<byte[]> ReadFrameAsync(Stream stream, CancellationToken cancellationToken)
    {
        var header = new byte[4];
        await stream.ReadExactlyAsync(header, cancellationToken);
        var length = BinaryPrimitives.ReadUInt32LittleEndian(header);
        if (length is 0 or > MaxFrameBytes) throw new InvalidDataException("Invalid IPC frame size.");
        var bytes = new byte[(int)length];
        await stream.ReadExactlyAsync(bytes, cancellationToken);
        return bytes;
    }

    public static async Task WriteFrameAsync(Stream stream, byte[] bytes, CancellationToken cancellationToken)
    {
        if (bytes.Length is 0 or > MaxFrameBytes) throw new InvalidDataException("Invalid IPC frame size.");
        var header = new byte[4];
        BinaryPrimitives.WriteUInt32LittleEndian(header, (uint)bytes.Length);
        await stream.WriteAsync(header, cancellationToken);
        await stream.WriteAsync(bytes, cancellationToken);
        await stream.FlushAsync(cancellationToken);
    }

    public ValueTask DisposeAsync() => _pipe.DisposeAsync();
}
