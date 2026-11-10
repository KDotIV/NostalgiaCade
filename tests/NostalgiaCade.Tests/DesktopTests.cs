using Avalonia;
using Avalonia.Controls;
using Avalonia.Headless;
using Avalonia.Headless.XUnit;
using Avalonia.Input;
using Avalonia.Media.Imaging;
using Avalonia.Threading;
using System.Security.Cryptography;
using System.Text.Json;
using NostalgiaCade.Client;
using NostalgiaCade.Desktop;
using Xunit;

[assembly: AvaloniaTestApplication(typeof(NostalgiaCade.Tests.TestAppBuilder))]

namespace NostalgiaCade.Tests;

public class TestAppBuilder
{
    public static AppBuilder BuildAvaloniaApp() => AppBuilder.Configure<App>()
        .UseSkia().WithInterFont()
        .UseHeadless(new AvaloniaHeadlessPlatformOptions { UseHeadlessDrawing = false });
}

public class DesktopTests
{
    [AvaloniaFact]
    public async Task LibraryLoadsFromRealCoreAndSupportsKeyboardNavigation()
    {
        var executable = Environment.GetEnvironmentVariable("NCADE_TEST_CORE_PATH");
        Assert.True(File.Exists(executable), "Run scripts/Test.ps1 to build and locate the Go core.");
        var testRoot = Path.Combine(Path.GetTempPath(), "NostalgiaCade-tests", Guid.NewGuid().ToString("N"));
        var directory = Path.Combine(testRoot,"state");
        var session = new CoreSession(directory, executable!);
        var viewModel = new MainViewModel(session);
        var window = new MainWindow(viewModel);
        var closed = new TaskCompletionSource(TaskCreationOptions.RunContinuationsAsynchronously);
        window.Closed += (_, _) => closed.TrySetResult();
        try
        {
            var connection = await session.ConnectAsync();
            var paths = new { emulators=Path.Combine(testRoot,"emulators"),roms=Path.Combine(testRoot,"roms"),firmware=Path.Combine(testRoot,"firmware"),saves=Path.Combine(testRoot,"saves"),settings=Path.Combine(testRoot,"settings"),cache=Path.Combine(testRoot,"cache") };
            await connection.CallAsync<JsonElement>("setup.configure",new { paths, downloadHosts = Array.Empty<string>() });
            var emulatorPath=Path.Combine(paths.emulators,"Dolphin.exe");
            File.Copy(Path.Combine(Path.GetDirectoryName(executable)!,"ncade-test-emulator.exe"),emulatorPath);
            await connection.CallAsync<JsonElement>("emulators.register",new {id="dolphin",adapter="dolphin",executable=emulatorPath,version="test-fixture"});
            var source=Path.Combine(testRoot,"homebrew.dol");var bytes="UI test content"u8.ToArray();await File.WriteAllBytesAsync(source,bytes);
            var operation=await connection.CallAsync<JsonElement>("operations.submit",new {key="ui-import",kind="import",import=new {genreId="fighting",reference=false,entry=new {id="sample",title="Local homebrew sample",emulatorId="dolphin",filename="homebrew.dol",sha256=Convert.ToHexStringLower(SHA256.HashData(bytes)),bytes=bytes.Length,source=new {kind="local",path=source}}}});
            using var deadline=new CancellationTokenSource(TimeSpan.FromSeconds(10));
            while(operation.GetProperty("state").GetString() is "pending" or "running")
            {
                await Task.Delay(50,deadline.Token);
                operation=await connection.CallAsync<JsonElement>("operations.get",new {id=operation.GetProperty("id").GetString()},deadline.Token);
            }
            Assert.Equal("completed",operation.GetProperty("state").GetString());
            window.Show();
            await viewModel.RefreshCommand.ExecutionTask!.WaitAsync(TimeSpan.FromSeconds(20));
            Dispatcher.UIThread.RunJobs();
            Assert.False(viewModel.HasError, viewModel.Error);
            Assert.True(viewModel.IsConnected);
            Assert.Equal(5, viewModel.Genres.Count);
            var genres = window.FindControl<ListBox>("GenreList")!;
            Assert.Equal(5, genres.ItemCount);
            genres.ContainerFromIndex(0)!.Focus();
            window.KeyPress(Key.Down, RawInputModifiers.None, PhysicalKey.ArrowDown, null);
            window.KeyRelease(Key.Down, RawInputModifiers.None, PhysicalKey.ArrowDown, null);
            Assert.Equal("Fighting", viewModel.SelectedGenreName);
            Assert.Single(viewModel.Games);
            Assert.True(viewModel.PlayCommand.CanExecute(null));
            Capture(window, "library.png");

            window.FindControl<Button>("SystemButton")!.Focus();
            window.KeyPressQwerty(PhysicalKey.Space, RawInputModifiers.None);
            window.KeyReleaseQwerty(PhysicalKey.Space, RawInputModifiers.None);
            Assert.True(viewModel.ShowSystem);
            Assert.False(viewModel.ShowLibrary);
            Capture(window, "system.png");

            window.KeyPress(Key.F11, RawInputModifiers.None, PhysicalKey.F11, null);
            window.KeyRelease(Key.F11, RawInputModifiers.None, PhysicalKey.F11, null);
            Assert.Equal(WindowState.FullScreen, window.WindowState);
        }
        finally
        {
            window.Close();
            await closed.Task.WaitAsync(TimeSpan.FromSeconds(10));
            if (Directory.Exists(testRoot)) Directory.Delete(testRoot, recursive: true);
        }
    }

    private static void Capture(Window window, string name)
    {
        using var frame = window.CaptureRenderedFrame();
        Assert.NotNull(frame);
        Assert.True(frame.PixelSize.Width >= 920);
        if (Environment.GetEnvironmentVariable("NCADE_TEST_SCREENSHOTS") is { Length: > 0 } directory)
        {
            Directory.CreateDirectory(directory);
            frame.Save(Path.Combine(directory, name), new PngBitmapEncoderOptions());
        }
    }
}
