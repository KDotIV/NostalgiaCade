using System.Diagnostics;
using System.Text.Json;
using NostalgiaCade.Client;
using Xunit;

namespace NostalgiaCade.Tests;

public class CoreIntegrationTests
{
    [Fact]
    public async Task ReconnectsAfterCoreCrashAndReplacesStaleEndpoint()
    {
        var executable = Environment.GetEnvironmentVariable("NCADE_TEST_CORE_PATH");
        Assert.True(File.Exists(executable), "Run scripts/Test.ps1 first.");
        var directory = Path.Combine(Path.GetTempPath(), "NostalgiaCade-tests", Guid.NewGuid().ToString("N"));
        Directory.CreateDirectory(directory);
        using var deadline = new CancellationTokenSource(TimeSpan.FromSeconds(30));
        // The core must replace stale discovery data while starting.
        await File.WriteAllTextAsync(Path.Combine(directory, "core-endpoint.json"), "{\"pipeName\":null,\"protocolVersion\":1}", deadline.Token);
        var start = new ProcessStartInfo(executable!) { UseShellExecute = false, CreateNoWindow = true };
        start.ArgumentList.Add("--data-dir");
        start.ArgumentList.Add(directory);
        using var initial = Process.Start(start)!;
        try
        {
            await using (var session = new CoreSession(directory, executable!))
            {
                // Wait until the explicitly owned test process has published its endpoint.
                var endpoint = Path.Combine(directory, "core-endpoint.json");
                while (!(await File.ReadAllTextAsync(endpoint, deadline.Token)).Contains("NostalgiaCade.", StringComparison.Ordinal))
                    await Task.Delay(50, deadline.Token);
                var first = await session.ConnectAsync(deadline.Token);
                Assert.NotNull(await first.CallAsync<CoreStatus>("system.status", cancellationToken: deadline.Token));
                initial.Kill(); // Only the process this test directly launched.
                await initial.WaitForExitAsync(deadline.Token);
                var recovered = await session.ConnectAsync(deadline.Token);
                Assert.Equal(5, (await recovered.CallAsync<Genre[]>("catalog.genres", cancellationToken: deadline.Token)).Length);
            }
        }
        finally
        {
            if (!initial.HasExited) { initial.Kill(); await initial.WaitForExitAsync(deadline.Token); }
            Directory.Delete(directory, recursive: true);
        }
    }

    [Fact]
    public async Task DesktopClientStartsConnectsReattachesAndStopsRealCore()
    {
        var executable = Environment.GetEnvironmentVariable("NCADE_TEST_CORE_PATH");
        Assert.True(File.Exists(executable), "Build the Go core and set NCADE_TEST_CORE_PATH; scripts/Test.ps1 does this.");
        var dataDirectory = Path.Combine(Path.GetTempPath(), "NostalgiaCade-tests", Guid.NewGuid().ToString("N"));
        Directory.CreateDirectory(dataDirectory);
        using var deadline = new CancellationTokenSource(TimeSpan.FromSeconds(30));
        try
        {
            // The desktop must recover from malformed discovery data before launch.
            await File.WriteAllTextAsync(Path.Combine(dataDirectory, "core-endpoint.json"), "{\"pipeName\":null,\"protocolVersion\":1}", deadline.Token);
            await using (var owner = new CoreSession(dataDirectory, executable!))
            {
                var connection = await owner.ConnectAsync(deadline.Token);
                var status = await connection.CallAsync<CoreStatus>("system.status", cancellationToken: deadline.Token);
                Assert.Equal(1, status.ProtocolVersion);
                Assert.Equal("005_emulator_adapters.sql", status.DatabaseVersion);
                Assert.Equal("empty", status.Readiness);
                Assert.Equal(0, status.LocalGameCount);
                var genres = await connection.CallAsync<Genre[]>("catalog.genres", cancellationToken: deadline.Token);
                Assert.Equal(5, genres.Length);
                Assert.Equal("racing", genres[0].Id);

                // A second desktop attaches to the existing core, even when its
                // candidate executable does not exist. It must not stop the owner.
                await using (var second = new CoreSession(dataDirectory, "missing-core.exe"))
                {
                    var other = await second.ConnectAsync(deadline.Token);
                    Assert.Equal(status.DatabaseVersion, (await other.CallAsync<CoreStatus>("system.status", cancellationToken: deadline.Token)).DatabaseVersion);
                }
                Assert.NotNull(await connection.CallAsync<CoreStatus>("system.status", cancellationToken: deadline.Token));

                var rpcError = await Assert.ThrowsAsync<CoreRpcException>(() => connection.CallAsync<JsonElement>("catalog.addGame", cancellationToken: deadline.Token));
                Assert.Equal(-32601, rpcError.Code);

                var start = new ProcessStartInfo(executable!) { UseShellExecute = false, CreateNoWindow = true, RedirectStandardError = true };
                start.ArgumentList.Add("--data-dir");
                start.ArgumentList.Add(dataDirectory);
                using var duplicate = Process.Start(start)!;
                var errorTask = duplicate.StandardError.ReadToEndAsync(deadline.Token);
                await duplicate.WaitForExitAsync(deadline.Token);
                Assert.NotEqual(0, duplicate.ExitCode);
                Assert.Contains("already owns", await errorTask);
            }
            Assert.False(File.Exists(Path.Combine(dataDirectory, "core-endpoint.json")));

            // Restart against existing SQLite state and reapply migrations safely.
            await using (var restarted = new CoreSession(dataDirectory, executable!))
            {
                var connection = await restarted.ConnectAsync(deadline.Token);
                Assert.Equal(5, (await connection.CallAsync<Genre[]>("catalog.genres", cancellationToken: deadline.Token)).Length);
            }
        }
        finally
        {
            // Only this test's explicitly created GUID directory is owned here.
            Directory.Delete(dataDirectory, recursive: true);
        }
    }
}
