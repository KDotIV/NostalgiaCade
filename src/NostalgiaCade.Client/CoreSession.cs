using System.Diagnostics;
using System.Text.Json;

namespace NostalgiaCade.Client;

/// <summary>Discovers a running core or starts the bundled executable. Reconnect
/// never terminates an unrelated process or trusts the endpoint's PID for kills.</summary>
public sealed class CoreSession(string dataDirectory, string executablePath) : IAsyncDisposable
{
    private static readonly JsonSerializerOptions Json = new(JsonSerializerDefaults.Web);
    private readonly SemaphoreSlim _connectGate = new(1, 1);
    private Process? _ownedProcess;
    private int? _connectedProcessId;
    private CoreConnection? _connection;
    private string? _startupError;

    public string DataDirectory { get; } = Path.GetFullPath(dataDirectory);

    public async Task<CoreConnection> ConnectAsync(CancellationToken cancellationToken = default)
    {
        await _connectGate.WaitAsync(cancellationToken);
        try
        {
            if (_connection is not null)
            {
                try { await _connection.CallAsync<CoreStatus>("system.status", cancellationToken: cancellationToken); return _connection; }
                catch (Exception e) when (IsConnectionFailure(e)) { await _connection.DisposeAsync(); _connection = null; }
            }
            Directory.CreateDirectory(DataDirectory);
            var discovered = await TryDiscoverAsync(cancellationToken);
            if (discovered is not null) return _connection = discovered;
            cancellationToken.ThrowIfCancellationRequested();
            if (!File.Exists(executablePath)) throw new FileNotFoundException("The Go core executable is missing. Run scripts/Build.ps1 first.", executablePath);

            if (_ownedProcess is null || _ownedProcess.HasExited)
            {
                _ownedProcess?.Dispose();
                var start = new ProcessStartInfo(Path.GetFullPath(executablePath))
                {
                    UseShellExecute = false, CreateNoWindow = true, RedirectStandardError = true
                };
                start.ArgumentList.Add("--data-dir");
                start.Environment.Remove("NCADE_POSTGRES_DSN");
                start.Environment.Remove("NCADE_TEST_POSTGRES_DSN");
                start.Environment.Remove("NCADE_ADMIN_TOKEN");
                start.Environment.Remove("NCADE_ADMIN_SUBJECT");
                start.ArgumentList.Add(DataDirectory);
                _startupError = null;
                _ownedProcess = new Process { StartInfo = start };
                _ownedProcess.ErrorDataReceived += (_, args) => { if (args.Data is not null) _startupError = args.Data; };
                _ownedProcess.Start();
                _ownedProcess.BeginErrorReadLine();
            }

            using var startup = CancellationTokenSource.CreateLinkedTokenSource(cancellationToken);
            startup.CancelAfter(TimeSpan.FromSeconds(15));
            while (!startup.IsCancellationRequested)
            {
                discovered = await TryDiscoverAsync(startup.Token);
                if (discovered is not null) return _connection = discovered;
                if (_ownedProcess.HasExited)
                    throw new IOException("The Go core could not start. " + (_startupError ?? "Check data-directory access."));
                await Task.Delay(100, startup.Token);
            }
            throw new TimeoutException("The local core did not become ready in time.");
        }
        finally { _connectGate.Release(); }
    }

    private async Task<CoreConnection?> TryDiscoverAsync(CancellationToken cancellationToken)
    {
        var path = Path.Combine(DataDirectory, "core-endpoint.json");
        if (!File.Exists(path)) return null;
        try
        {
            // Endpoint files are tiny and read from our own data directory.
            if (new FileInfo(path).Length > 4096) throw new InvalidDataException("Invalid core endpoint file.");
            var endpoint = JsonSerializer.Deserialize<CoreEndpoint>(await File.ReadAllTextAsync(path, cancellationToken), Json)
                ?? throw new InvalidDataException("Empty core endpoint file.");
            if (endpoint.ProtocolVersion != CoreConnection.ProtocolVersion)
                throw new CoreRpcException(-32001, "A different core version is running. Close it before updating the desktop.");
            var connection = await CoreConnection.ConnectAsync(endpoint.PipeName, cancellationToken);
            _connectedProcessId = endpoint.ProcessId;
            return connection;
        }
        catch (Exception e) when (e is IOException or InvalidDataException or TimeoutException or JsonException) { return null; }
    }

    private static bool IsConnectionFailure(Exception e) => e is IOException or InvalidDataException or JsonException or TimeoutException or ObjectDisposedException or OperationCanceledException;

    public async ValueTask DisposeAsync()
    {
        if (_ownedProcess is { HasExited: false } && _connectedProcessId == _ownedProcess.Id && _connection is not null)
        {
            var canWait = true;
            try { await _connection.CallAsync<JsonElement>("system.shutdown"); }
            catch (CoreRpcException) { canWait = false; } // Busy: keep the game/operation alive.
            catch (Exception e) when (IsConnectionFailure(e)) { } // It may already be exiting.
            if (canWait)
            {
                using var timeout = new CancellationTokenSource(TimeSpan.FromSeconds(3));
                try { await _ownedProcess.WaitForExitAsync(timeout.Token); }
                catch (OperationCanceledException) { /* Leave a recoverable core, never kill an emulator tree. */ }
            }
        }
        if (_connection is not null) await _connection.DisposeAsync();
        _ownedProcess?.Dispose();
    }
}
