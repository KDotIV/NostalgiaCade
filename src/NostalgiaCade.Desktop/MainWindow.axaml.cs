using Avalonia.Controls;
using Avalonia.Input;
using Avalonia.Threading;

namespace NostalgiaCade.Desktop;

public partial class MainWindow : Window
{
    private readonly MainViewModel _viewModel;
    private bool _canClose;
    private bool _closing;

    public MainWindow() : this(new MainViewModel()) { }

    public MainWindow(MainViewModel viewModel)
    {
        _viewModel = viewModel;
        InitializeComponent();
        DataContext = _viewModel;
        var sessionTimer = new DispatcherTimer { Interval = TimeSpan.FromSeconds(2) };
        sessionTimer.Tick += async (_, _) => { if (!_closing && !_viewModel.PollSessionCommand.IsRunning) await _viewModel.PollSessionCommand.ExecuteAsync(null); };
        _viewModel.GameEnded += (_, _) => { if (!_closing) { Activate(); this.FindControl<ListBox>("GameList")?.Focus(); } };
        KeyDown += (_, args) =>
        {
            if (args.Key != Key.F11) return;
            WindowState = WindowState == WindowState.FullScreen ? WindowState.Normal : WindowState.FullScreen;
            args.Handled = true;
        };
        Opened += async (_, _) => { await _viewModel.RefreshCommand.ExecuteAsync(null); if (!_closing) sessionTimer.Start(); };
        Closing += async (_, args) =>
        {
            if (_canClose) return;
            args.Cancel = true;
            if (_closing) return;
            _closing = true;
            sessionTimer.Stop();
            IsEnabled = false;
            await _viewModel.DisposeAsync();
            _canClose = true;
            Close();
        };
    }
}
