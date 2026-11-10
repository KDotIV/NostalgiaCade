using System.Diagnostics;
using System.IO.Compression;
using System.Security.Cryptography;
using System.Text.Json;
using System.Text.Json.Nodes;
using NostalgiaCade.Client;
using Xunit;

namespace NostalgiaCade.Tests;

public class AdapterIntegrationTests
{
    [Theory]
    [InlineData("duckstation", ".chd")]
    [InlineData("pcsx2", ".iso")]
    [InlineData("rpcs3", ".bin")]
    [InlineData("eden", ".nro")]
    public async Task OnboardLaunchConfigureAndExitUseIsolatedProfiles(string adapter, string extension)
    {
        var core = Environment.GetEnvironmentVariable("NCADE_TEST_CORE_PATH")!;
        var cli = Path.Combine(Path.GetDirectoryName(core)!, "ncade.exe");
        var fixture = Path.Combine(Path.GetDirectoryName(core)!, "ncade-test-emulator.exe");
        var root = Path.Combine(Path.GetTempPath(), "NostalgiaCade adapter tests", Guid.NewGuid().ToString("N"));
        Directory.CreateDirectory(root);
        using var timeout = new CancellationTokenSource(TimeSpan.FromSeconds(90));
        var token = timeout.Token;
        await using var session = new CoreSession(root, core);
        CoreConnection? connection = null;
        try
        {
            await Cli("setup", "--root", root);
            var archivePath = Path.Combine(root, "fixture.zip");
            using (var archive = ZipFile.Open(archivePath, ZipArchiveMode.Create))
                archive.CreateEntryFromFile(fixture, adapter + ".exe");
            var catalog = JsonNode.Parse(await Cli("emulator", "catalog"))!;
            var package = catalog["packages"]!.AsArray().Single(p => p!["slug"]!.GetValue<string>() == adapter)!.DeepClone();
            package["version"] = "integration-fixture";
            package["format"] = "zip";
            package["executable"] = adapter + ".exe";
            package["bytes"] = new FileInfo(archivePath).Length;
            package["sha256"] = Convert.ToHexStringLower(SHA256.HashData(await File.ReadAllBytesAsync(archivePath, token)));
            catalog["packages"] = new JsonArray(package);
            var catalogPath = Path.Combine(root, "catalog.json");
            await File.WriteAllTextAsync(catalogPath, catalog.ToJsonString(), token);
            await Cli("emulator", "install", "--catalog", catalogPath, "--id", adapter, "--archive", archivePath);
            var preview = JsonNode.Parse(await Cli("emulator", "prepare", "--id", adapter))!;
            Assert.False(preview["prepared"]!.GetValue<bool>());
            await Cli("emulator", "prepare", "--id", adapter, "--apply");
            if (adapter is "duckstation" or "pcsx2")
            {
                var bios = Path.Combine(root, "bios-fixture.bin");
                await File.WriteAllBytesAsync(bios, new byte[adapter == "pcsx2" ? 4 * 1024 * 1024 : 512 * 1024], token);
                await Cli("emulator", "firmware", "--id", adapter, "--kind", "bios", "--path", bios);
            }
            if (adapter == "rpcs3")
            {
                var flash = Path.Combine(root, "flash-fixture");
                Directory.CreateDirectory(Path.Combine(flash, "vsh", "module"));
                await File.WriteAllTextAsync(Path.Combine(flash, "vsh", "module", "vsh.self"), "structural test only", token);
                await Cli("emulator", "firmware", "--id", adapter, "--kind", "ps3-flash", "--path", flash);
            }
            var home = Path.Combine(root, "Settings", adapter);
            var fixtureHome = adapter is "duckstation" or "eden" ? Path.Combine(home, "runtime") : home;
            if (adapter == "pcsx2") fixtureHome = Path.Combine(home, "PCSX2");
            var savedProgress = Path.Combine(root, "Saves", adapter, "existing-save");
            await File.WriteAllTextAsync(savedProgress, "preserved", token);
            var source = Path.Combine(root, adapter == "rpcs3" ? "EBOOT.BIN" : "Game with spaces" + extension);
            await File.WriteAllTextAsync(source, "synthetic game fixture", token);
            var arguments = new List<string> { "import", "--path", source, "--genre", "action-adventure", "--emulator", adapter };
            if (adapter == "rpcs3") arguments.Add("--reference");
            await Cli(arguments.ToArray());
            connection = await session.ConnectAsync(token);
            var game = Assert.Single(await connection.CallAsync<LocalGame[]>("library.games", cancellationToken: token));
            var state = await connection.CallAsync<JsonElement>("session.launch", new { id = game.Id }, token);
            Assert.Equal("game", state.GetProperty("purpose").GetString());
            await WaitForReady();
            var args = JsonSerializer.Deserialize<string[]>(await File.ReadAllTextAsync(Path.Combine(fixtureHome, "fixture-args.json"), token))!;
            Assert.Contains(game.Path, args);
            Assert.Equal("false", await File.ReadAllTextAsync(Path.Combine(fixtureHome, "fixture-catalog-credential.json"), token));
            var startup = JsonNode.Parse(await File.ReadAllTextAsync(Path.Combine(fixtureHome, "fixture-startup.json"), token))!;
            Assert.Equal(@"winsta0\default", startup["desktop"]!.GetValue<string>().ToLowerInvariant());
            Assert.True(startup["explicitWindowState"]!.GetValue<bool>());
            Assert.Equal(1, startup["showWindow"]!.GetValue<int>());
            Assert.Contains(adapter switch { "duckstation" => "-batch", "pcsx2" => "-datapath", "rpcs3" => "--no-gui", _ => "-g" }, args);
            await Stop();
            File.Delete(Path.Combine(fixtureHome, "fixture-ready"));
            state = await connection.CallAsync<JsonElement>("emulators.configure", new { id = adapter }, token);
            Assert.Equal("configure", state.GetProperty("purpose").GetString());
            await WaitForReady();
            var blocked = await Assert.ThrowsAsync<CoreRpcException>(() => connection.CallAsync<JsonElement>("session.launch", new { id = game.Id }, token));
            Assert.Contains("game_running", blocked.Message);
            await Stop();
            Assert.Equal("preserved", await File.ReadAllTextAsync(savedProgress, token));
            Assert.Equal("synthetic game fixture", await File.ReadAllTextAsync(source, token));
            await Cli("emulator", "prepare", "--id", adapter, "--apply");
        }
        finally
        {
            using var cleanup = new CancellationTokenSource(TimeSpan.FromSeconds(10));
            token = cleanup.Token;
            if (connection is not null)
            {
                try { await connection.CallAsync<JsonElement>("session.stop", new { force = true }, token); } catch (Exception) { }
                try
                {
                    while ((await connection.CallAsync<PlaySession>("session.status", cancellationToken: token)).State == "running") await Task.Delay(50, token);
                    await connection.CallAsync<JsonElement>("system.shutdown", cancellationToken: token);
                    while (File.Exists(Path.Combine(root, "core-endpoint.json"))) await Task.Delay(50, token);
                }
                catch (Exception) { }
            }
            await session.DisposeAsync();
            Directory.Delete(root, recursive: true);
        }
        async Task<string> Cli(params string[] args)
        {
            var start = new ProcessStartInfo(cli) { UseShellExecute = false, CreateNoWindow = true, RedirectStandardOutput = true, RedirectStandardError = true };
            start.Environment["NCADE_POSTGRES_DSN"] = "fixture-sentinel";
            start.ArgumentList.Add("--data-dir"); start.ArgumentList.Add(root);
            foreach (var arg in args) start.ArgumentList.Add(arg);
            using var process = Process.Start(start)!;
            var output = process.StandardOutput.ReadToEndAsync(token);
            var errors = process.StandardError.ReadToEndAsync(token);
            await process.WaitForExitAsync(token);
            Assert.True(process.ExitCode == 0, await output + "\n" + await errors);
            return await output;
        }
        async Task WaitForReady()
        {
            var folder = Path.Combine(root, "Settings", adapter);
            if (adapter is "duckstation" or "eden") folder = Path.Combine(folder, "runtime");
            if (adapter == "pcsx2") folder = Path.Combine(folder, "PCSX2");
            while (!File.Exists(Path.Combine(folder, "fixture-ready"))) await Task.Delay(50, token);
        }
        async Task Stop()
        {
            await connection!.CallAsync<JsonElement>("session.stop", new { force = false }, token);
            PlaySession state;
            do { await Task.Delay(50, token); state = await connection.CallAsync<PlaySession>("session.status", cancellationToken: token); } while (state.State == "running");
            Assert.Equal(0, state.ExitCode);
        }
    }
}
