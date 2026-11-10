using Avalonia.Controls;
using Avalonia.Input;
using Avalonia.Threading;

namespace NostalgiaCade.Desktop;

public partial class MainWindow : Window
{
    private readonly MainViewModel _viewModel;
    private bool _canClose;
    private bool _closing;
    private CabinetInput? _cabinetInput;
    private CabinetControls _controls = new();
    private WindowState _libraryWindowState = WindowState.Normal;

    public MainWindow() : this(new MainViewModel()) { }

    public MainWindow(MainViewModel viewModel)
    {
        _viewModel = viewModel;
        InitializeComponent();
        DataContext = _viewModel;
        var sessionTimer = new DispatcherTimer { Interval = TimeSpan.FromSeconds(2) };
        sessionTimer.Tick += async (_, _) => { if (!_closing && !_viewModel.PollSessionCommand.IsRunning) await _viewModel.PollSessionCommand.ExecuteAsync(null); };
        _viewModel.GameEnded += (_, _) => ReturnToLibrary();
        _viewModel.ResumeRequested += (_, _) => { _libraryWindowState = WindowState; WindowState = WindowState.Minimized; };
        _viewModel.PropertyChanged += (_, args) => {
            if (args.PropertyName == nameof(MainViewModel.RunningGenreId) && _cabinetInput is not null)
                _viewModel.ReturnControlHint = _cabinetInput.Configure(_controls.ForGenre(_viewModel.RunningGenreId));
            if (args.PropertyName == nameof(MainViewModel.ShowSwitchConfirmation))
                Dispatcher.UIThread.Post(() => {
                    if (_viewModel.ShowSwitchConfirmation) this.FindControl<Button>("ConfirmSwitchButton")?.Focus();
                    else this.FindControl<ListBox>("GameList")?.Focus();
                });
        };
        KeyDown += (_, args) =>
        {
            if (args.Key != Key.F11) return;
            WindowState = WindowState == WindowState.FullScreen ? WindowState.Normal : WindowState.FullScreen;
            args.Handled = true;
        };
        Opened += async (_, _) => {
            string? controlsError = null;
            try { _controls = CabinetControls.Load(_viewModel.DataDirectory); }
            catch (Exception e) when (e is IOException or InvalidDataException or UnauthorizedAccessException or System.Text.Json.JsonException) {
                controlsError = $"Cabinet controls could not be loaded; default bindings are active: {e.Message}";
            }
            _cabinetInput = new CabinetInput(this, ReturnToLibrary, () => _viewModel.RunningProcessId, NavigateLibrary);
            _viewModel.ReturnControlHint = _cabinetInput.Configure(_controls.Default);
            await _viewModel.RefreshCommand.ExecuteAsync(null);
            if (controlsError is not null) { _viewModel.HasError = true; _viewModel.Error = controlsError; }
            if (!_closing) sessionTimer.Start();
        };
        Closing += async (_, args) =>
        {
            if (_canClose) return;
            args.Cancel = true;
            if (_closing) return;
            _closing = true;
            _cabinetInput?.Dispose();
            sessionTimer.Stop();
            IsEnabled = false;
            await _viewModel.DisposeAsync();
            _canClose = true;
            Close();
        };
    }

    private void ReturnToLibrary()
    {
        if (_closing) return;
        _viewModel.OpenLibraryCommand.Execute(null);
        if (WindowState == WindowState.Minimized) WindowState = _libraryWindowState;
        Show(); Activate();
        this.FindControl<ListBox>("GameList")?.Focus();
    }

    private void NavigateLibrary(CabinetMenuAction action)
    {
        if (_closing || !_viewModel.ShowLibrary) return;
        if (action == CabinetMenuAction.Back)
        {
            if (_viewModel.ShowSwitchConfirmation || _viewModel.IsStopping) _viewModel.CancelChangeGameCommand.Execute(null);
            else if (_viewModel.IsPlaying) _viewModel.ResumeGameCommand.Execute(null);
            return;
        }
        if (action == CabinetMenuAction.Accept)
        {
            System.Windows.Input.ICommand command = _viewModel.ShowSwitchConfirmation ? _viewModel.ConfirmChangeGameCommand :
                _viewModel.IsPlaying ? _viewModel.ChangeGameCommand : _viewModel.PlayCommand;
            if (command.CanExecute(null)) command.Execute(null);
            return;
        }
        if (_viewModel.ShowSwitchConfirmation) return;
        if (action is CabinetMenuAction.PreviousGenre or CabinetMenuAction.NextGenre && _viewModel.Genres.Count > 0)
        {
            var index = _viewModel.SelectedGenre is { } genre ? _viewModel.Genres.IndexOf(genre) : 0;
            index = Math.Clamp(index + (action == CabinetMenuAction.NextGenre ? 1 : -1), 0, _viewModel.Genres.Count - 1);
            if (index >= 0) _viewModel.SelectedGenre = _viewModel.Genres[index];
        }
        else if (_viewModel.Games.Count > 0)
        {
            var index = _viewModel.SelectedGame is { } game ? _viewModel.Games.IndexOf(game) : 0;
            index = Math.Clamp(index + (action == CabinetMenuAction.Down ? 1 : -1), 0, _viewModel.Games.Count - 1);
            _viewModel.SelectedGame = _viewModel.Games[index];
            this.FindControl<ListBox>("GameList")?.ScrollIntoView(_viewModel.SelectedGame);
        }
    }
}
