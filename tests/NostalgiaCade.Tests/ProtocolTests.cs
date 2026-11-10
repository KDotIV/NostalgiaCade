using System.Buffers.Binary;
using NostalgiaCade.Client;
using Xunit;

namespace NostalgiaCade.Tests;

public class ProtocolTests
{
    [Fact]
    public async Task ReadsFragmentedFrames()
    {
        using var buffer = new MemoryStream();
        await CoreConnection.WriteFrameAsync(buffer, "{\"id\":1}"u8.ToArray(), CancellationToken.None);
        using var input = new FragmentedStream(buffer.ToArray());
        Assert.Equal("{\"id\":1}"u8.ToArray(), await CoreConnection.ReadFrameAsync(input, CancellationToken.None));
    }

    [Theory]
    [InlineData(0u)]
    [InlineData(1048577u)]
    [InlineData(uint.MaxValue)]
    public async Task RejectsInvalidFrameLengths(uint size)
    {
        var header = new byte[4];
        BinaryPrimitives.WriteUInt32LittleEndian(header, size);
        using var input = new MemoryStream(header);
        await Assert.ThrowsAsync<InvalidDataException>(() => CoreConnection.ReadFrameAsync(input, CancellationToken.None));
    }

    [Fact]
    public async Task RejectsTruncatedFrames()
    {
        using var input = new MemoryStream([8, 0, 0, 0, 1]);
        await Assert.ThrowsAsync<EndOfStreamException>(() => CoreConnection.ReadFrameAsync(input, CancellationToken.None));
    }

    private sealed class FragmentedStream(byte[] bytes) : MemoryStream(bytes)
    {
        public override ValueTask<int> ReadAsync(Memory<byte> buffer, CancellationToken cancellationToken = default)
            => base.ReadAsync(buffer[..Math.Min(buffer.Length, 1)], cancellationToken);
    }
}
