using System.Diagnostics;
using System.IO.Compression;
using System.Security.Cryptography;
using System.Text.Json;
using System.Text.Json.Nodes;
using NostalgiaCade.Client;
using Xunit;

namespace NostalgiaCade.Tests;

public class LibraryIntegrationTests
{
    [Theory]
    [InlineData(false)]
    [InlineData(true)]
    public async Task CliSetupImportAndDolphinProcessSurviveCoreCrash(bool singleRoot)
    {
        var core = Environment.GetEnvironmentVariable("NCADE_TEST_CORE_PATH")!;
        Assert.True(File.Exists(core));
        var cli = Path.Combine(Path.GetDirectoryName(core)!, "ncade.exe");
        var fixture = Path.Combine(Path.GetDirectoryName(core)!, "ncade-test-emulator.exe");
        var root = Path.Combine(Path.GetTempPath(), "NostalgiaCade tests", Guid.NewGuid().ToString("N"));
        var data = singleRoot ? root : Path.Combine(root, "state");
        Directory.CreateDirectory(root);
        using var timeout = new CancellationTokenSource(TimeSpan.FromSeconds(60));
        var token = timeout.Token;
        var json = new JsonSerializerOptions(JsonSerializerDefaults.Web);
        var paths = new { emulators = Path.Combine(root,"Emulators"), roms = Path.Combine(root,"ROMs"), firmware = Path.Combine(root,"Firmware"),
            saves = Path.Combine(root,"Saves"), settings = Path.Combine(root,"Settings"), cache = Path.Combine(root,"Cache") };
        var configuration = Path.Combine(root,"setup.json");
        await File.WriteAllTextAsync(configuration, JsonSerializer.Serialize(new { paths, downloadHosts = Array.Empty<string>() }), token);
        CoreConnection? connection = null;
        var session = new CoreSession(data, core);
        try
        {
            var answers = singleRoot ? new[] { "1", root, "", "yes", "" }
                : new[] { "2", paths.emulators, paths.roms, paths.firmware, paths.saves, paths.settings, paths.cache, "", "yes", "" };
            await RunCliInput(string.Join(Environment.NewLine, answers), "setup", "--if-needed");
            if (singleRoot) await RunCli("setup", "--root", root);
            await RunCli("setup", "--config", configuration);
            if (singleRoot)
            {
                var archivePath = Path.Combine(root, "fixture.zip");
                using (var archive = ZipFile.Open(archivePath, ZipArchiveMode.Create))
                    archive.CreateEntryFromFile(fixture, "Dolphin.exe");
                var catalog = JsonNode.Parse(await RunCli("emulator", "catalog"))!;
                var package = catalog["packages"]!.AsArray().Single(p => p!["slug"]!.GetValue<string>() == "dolphin")!.DeepClone();
                package["version"] = "test-fixture";
                package["format"] = "zip";
                package["executable"] = "Dolphin.exe";
                package["bytes"] = new FileInfo(archivePath).Length;
                package["sha256"] = Convert.ToHexStringLower(SHA256.HashData(await File.ReadAllBytesAsync(archivePath, token)));
                catalog["packages"] = new JsonArray(package);
                var catalogPath = Path.Combine(root,"catalog.json");
                await File.WriteAllTextAsync(catalogPath,catalog.ToJsonString(),token);
                await RunCli("emulator","install","--catalog",catalogPath,"--id","dolphin","--archive",archivePath);
                var installed = JsonNode.Parse(await RunCli("emulator","list"))!.AsArray();
                Assert.Single(installed);
                Assert.True(installed[0]!["launchSupported"]!.GetValue<bool>());
            }
            else
            {
                File.Copy(fixture, Path.Combine(paths.emulators,"Dolphin.exe"));
                await RunCli("emulator", "add", "--exe", Path.Combine(paths.emulators,"Dolphin.exe"), "--version", "test-fixture");
            }
            var source = Path.Combine(root,"A game with spaces.dol");
            await File.WriteAllTextAsync(source,"synthetic homebrew fixture bytes",token);
            await RunCli("import", "--path", source, "--genre", "racing", "--title", "Test game");
            connection = await session.ConnectAsync(token);
            var games = await connection.CallAsync<LocalGame[]>("library.games", cancellationToken:token);
            var game = Assert.Single(games);
            Assert.Equal("local", game.Scope);
            Assert.StartsWith(paths.roms, game.Path);
            var state = await connection.CallAsync<PlaySession>("session.launch",new { id = game.Id },token);
            Assert.Equal("running",state.State);
            using var emulator = Process.GetProcessById(state.Pid);
            var ready = Path.Combine(paths.settings,"dolphin","fixture-ready");
            await Until(() => File.Exists(ready));
            Assert.Equal("false", await File.ReadAllTextAsync(Path.Combine(paths.settings,"dolphin","fixture-catalog-credential.json"),token));
            var args = JsonSerializer.Deserialize<string[]>(await File.ReadAllTextAsync(Path.Combine(paths.settings,"dolphin","fixture-args.json"),token))!;
            Assert.Contains(game.Path,args); // One argument even when the path contains spaces.
            Assert.Contains("Dolphin.General.NANDRootPath=" + Path.Combine(paths.saves,"dolphin","Wii"),args);
            var blocked = await Assert.ThrowsAsync<CoreRpcException>(() => connection.CallAsync<JsonElement>("system.shutdown", cancellationToken:token));
            Assert.Contains("game_running",blocked.Message);

            // Kill only the core whose endpoint was created by this isolated test.
            var endpoint = JsonSerializer.Deserialize<CoreEndpoint>(await File.ReadAllTextAsync(Path.Combine(data,"core-endpoint.json"),token),json)!;
            using (var crashedCore = Process.GetProcessById(endpoint.ProcessId))
            {
                crashedCore.Kill();
                await crashedCore.WaitForExitAsync(token);
            }
            Assert.False(emulator.HasExited);
            connection = await session.ConnectAsync(token);
            var recovered = await connection.CallAsync<PlaySession>("session.status", cancellationToken:token);
            Assert.Equal(state.Pid,recovered.Pid);
            Assert.Equal("running",recovered.State);
            await connection.CallAsync<JsonElement>("session.stop",new { force = false },token);
            await emulator.WaitForExitAsync(token);
            await UntilAsync(async () => (await connection.CallAsync<PlaySession>("session.status",cancellationToken:token)).State == "exited");
            Assert.Equal(0,(await connection.CallAsync<PlaySession>("session.status",cancellationToken:token)).ExitCode);
            Assert.Equal("shared save survives",await File.ReadAllTextAsync(Path.Combine(paths.saves,"dolphin","Wii","fixture-save.txt"),token));
            Assert.True(File.Exists(source));
            Assert.True(File.Exists(game.Path));
            var stuck = await connection.CallAsync<PlaySession>("session.launch",new { id = game.Id },token);
            using var stuckProcess = Process.GetProcessById(stuck.Pid);
            await connection.CallAsync<JsonElement>("session.stop",new { force = true },token);
            await stuckProcess.WaitForExitAsync(token);
            await UntilAsync(async () => (await connection.CallAsync<PlaySession>("session.status",cancellationToken:token)).State == "exited");
            Assert.Equal(1,(await connection.CallAsync<PlaySession>("session.status",cancellationToken:token)).ExitCode);
        }
        finally
        {
            using var cleanupTimeout = new CancellationTokenSource(TimeSpan.FromSeconds(10));
            token = cleanupTimeout.Token;
            if (connection is not null)
            {
                try { await connection.CallAsync<JsonElement>("session.stop", new { force = true }, token); } catch (Exception) { }
                try
                {
                    await UntilAsync(async () => (await connection.CallAsync<PlaySession>("session.status", cancellationToken:token)).State != "running");
                    var endpoint = JsonSerializer.Deserialize<CoreEndpoint>(await File.ReadAllTextAsync(Path.Combine(data,"core-endpoint.json"),token),json)!;
                    using var stoppingCore = Process.GetProcessById(endpoint.ProcessId);
                    await connection.CallAsync<JsonElement>("system.shutdown", cancellationToken:token);
                    await stoppingCore.WaitForExitAsync(token);
                }
                catch (Exception) { }
            }
            await session.DisposeAsync();
            Directory.Delete(root, recursive:true);
        }

        Task<string> RunCli(params string[] arguments) => RunCliInput(null, arguments);
        async Task<string> RunCliInput(string? input, params string[] arguments)
        {
            var start = new ProcessStartInfo(cli) { UseShellExecute = false, CreateNoWindow = true, RedirectStandardOutput = true, RedirectStandardError = true, RedirectStandardInput = true };
            start.Environment["NCADE_POSTGRES_DSN"] = "test-only-sentinel-not-a-connection";
            start.ArgumentList.Add("--data-dir"); start.ArgumentList.Add(data);
            foreach(var argument in arguments) start.ArgumentList.Add(argument);
            using var process = Process.Start(start)!;
            if (input is not null) await process.StandardInput.WriteAsync(input);
            process.StandardInput.Close();
            var stdout = process.StandardOutput.ReadToEndAsync(token);
            var stderr = process.StandardError.ReadToEndAsync(token);
            await process.WaitForExitAsync(token);
            Assert.True(process.ExitCode == 0, await stdout + "\n" + await stderr);
            return await stdout;
        }
        async Task Until(Func<bool> condition) { while (!condition()) await Task.Delay(50,token); }
        async Task UntilAsync(Func<Task<bool>> condition) { while (!await condition()) await Task.Delay(50,token); }
    }
}
