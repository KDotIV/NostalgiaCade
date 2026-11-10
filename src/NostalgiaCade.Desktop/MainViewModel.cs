using System.Collections.ObjectModel;
using System.Diagnostics;
using CommunityToolkit.Mvvm.ComponentModel;
using CommunityToolkit.Mvvm.Input;
using NostalgiaCade.Client;

namespace NostalgiaCade.Desktop;

public partial class MainViewModel : ObservableObject, IAsyncDisposable
{
    private readonly CoreSession _session;
    private readonly CancellationTokenSource _lifetime = new();
    private readonly bool _allowAutomaticSetup;
    private bool _setupAttempted;
    private LocalGame[] _allGames = [];
    private PlaySession? _currentSession;
    private LocalGame? _pendingGame;
    private string? _pendingSessionId;
    private DateTimeOffset? _stopRequestedAt;
    public ObservableCollection<Genre> Genres { get; } = [];
    public ObservableCollection<LocalGame> Games { get; } = [];
    public event EventHandler? GameEnded;
    public event EventHandler? ResumeRequested;

    [ObservableProperty] private string _connectionStatus = "Connecting to your cabinet…";
    [ObservableProperty] private string _error = "";
    [ObservableProperty] private bool _hasError;
    [ObservableProperty, NotifyCanExecuteChangedFor(nameof(PlayCommand))] private bool _isConnected;
    [ObservableProperty, NotifyCanExecuteChangedFor(nameof(PlayCommand))] private bool _setupRequired;
    [ObservableProperty, NotifyCanExecuteChangedFor(nameof(PlayCommand)), NotifyCanExecuteChangedFor(nameof(StopGameCommand))] private bool _isPlaying;
    [ObservableProperty, NotifyCanExecuteChangedFor(nameof(PlayCommand))] private LocalGame? _selectedGame;
    [ObservableProperty] private string _sessionDescription = "No game running";
    [ObservableProperty] private bool _showLibrary = true;
    [ObservableProperty] private bool _showSystem;
    [ObservableProperty] private Genre? _selectedGenre;
    [ObservableProperty] private string _coreVersion = "—";
    [ObservableProperty] private string _databaseVersion = "—";
    [ObservableProperty] private string _platform = "—";
    [ObservableProperty] private int _localGameCount;
    [ObservableProperty] private string _returnControlHint = "Ctrl+Alt+Home · Return to NCade";
    [ObservableProperty] private string? _runningGenreId;
    [ObservableProperty, NotifyCanExecuteChangedFor(nameof(PlayCommand)), NotifyCanExecuteChangedFor(nameof(ChangeGameCommand)), NotifyCanExecuteChangedFor(nameof(StopGameCommand))] private bool _isStopping;
    [ObservableProperty] private bool _showSwitchConfirmation;
    [ObservableProperty] private string _switchDescription = "";
    public int RunningProcessId => _currentSession?.State == "running" && _currentSession.Purpose == "game" ? _currentSession.Pid : 0;

    public MainViewModel() : this(new CoreSession(
        Environment.GetEnvironmentVariable("NCADE_DATA_DIR") ?? Path.Combine(Environment.GetFolderPath(Environment.SpecialFolder.LocalApplicationData), "NostalgiaCade"),
        Environment.GetEnvironmentVariable("NCADE_CORE_PATH") ?? Path.Combine(AppContext.BaseDirectory, "core", "ncade-core.exe"))) { _allowAutomaticSetup = true; }

    public MainViewModel(CoreSession session) => _session = session;
    public string DataDirectory => _session.DataDirectory;
    public string SelectedGenreName => SelectedGenre?.Name ?? "Your next collection";
    public bool HasGames => Games.Count > 0;
    public bool EmptyLibrary => !HasGames;
    partial void OnSelectedGenreChanged(Genre? value) { OnPropertyChanged(nameof(SelectedGenreName)); FilterGames(); }
    private void FilterGames()
    {
        var selectedId = SelectedGame?.Id;
        Games.Clear();
        foreach (var game in _allGames.Where(g => g.GenreId == SelectedGenre?.Id)) Games.Add(game);
        SelectedGame = Games.FirstOrDefault(g => g.Id == selectedId) ?? Games.FirstOrDefault();
        OnPropertyChanged(nameof(HasGames)); OnPropertyChanged(nameof(EmptyLibrary));
    }

    [RelayCommand]
    private void OpenLibrary() { ShowLibrary = true; ShowSystem = false; }
    [RelayCommand]
    private void OpenSystem() { ShowLibrary = false; ShowSystem = true; }

    [RelayCommand]
    private async Task RefreshAsync()
    {
        HasError = false;
        ConnectionStatus = "Checking your cabinet…";
        try
        {
            var connection = await _session.ConnectAsync(_lifetime.Token);
            var status = await connection.CallAsync<CoreStatus>("system.status", cancellationToken: _lifetime.Token);
            var genres = await connection.CallAsync<Genre[]>("catalog.genres", cancellationToken: _lifetime.Token);
            _allGames = await connection.CallAsync<LocalGame[]>("library.games", cancellationToken: _lifetime.Token);
            var selectedId = SelectedGenre?.Id;
            Genres.Clear();
            foreach (var genre in genres) Genres.Add(genre);
            SelectedGenre = Genres.FirstOrDefault(g => g.Id == selectedId) ?? Genres.FirstOrDefault();
            CoreVersion = status.Version;
            DatabaseVersion = status.DatabaseVersion;
            Platform = status.Platform;
            LocalGameCount = status.LocalGameCount;
            SetupRequired = status.SetupRequired;
            FilterGames();
            IsConnected = true;
            ConnectionStatus = "Cabinet connected";
            await PollSessionAsync();
            if (SetupRequired && _allowAutomaticSetup && !_setupAttempted)
            {
                _setupAttempted = true;
                await ConfigureStorageAsync();
            }
        }
        catch (Exception e) when (e is IOException or InvalidDataException or TimeoutException or OperationCanceledException or CoreRpcException or System.Text.Json.JsonException)
        {
            IsConnected = false;
            HasError = true;
            Error = e is OperationCanceledException ? "The connection timed out. Try reconnecting." : e.Message;
            ConnectionStatus = "Core unavailable";
        }
    }

    [RelayCommand]
    private async Task ConfigureStorageAsync()
    {
        try
        {
            var executable = Path.Combine(AppContext.BaseDirectory, "core", "ncade.exe");
            if (!File.Exists(executable)) throw new FileNotFoundException("Build the operator CLI before starting setup.");
            var start = new ProcessStartInfo(executable) { UseShellExecute = true };
            start.ArgumentList.Add("--data-dir"); start.ArgumentList.Add(DataDirectory);
            start.ArgumentList.Add("setup"); start.ArgumentList.Add("--if-needed");
            using var process = Process.Start(start) ?? throw new IOException("Could not open the setup console.");
            await process.WaitForExitAsync(_lifetime.Token);
            if (process.ExitCode != 0) throw new IOException("Setup was not completed. Run the storage guide again when ready.");
            await RefreshAsync();
        }
        catch (Exception e) when (IsUserFacingFailure(e)) { ShowError(e); }
    }
    partial void OnIsPlayingChanged(bool value) => ChangeGameCommand.NotifyCanExecuteChanged();
    partial void OnSelectedGameChanged(LocalGame? value) => ChangeGameCommand.NotifyCanExecuteChanged();
    private bool CanPlay() => IsConnected && !SetupRequired && !IsPlaying && !IsStopping && SelectedGame?.EmulatorId is { Length: > 0 };
    private bool CanStop() => IsPlaying && !IsStopping;
    private bool CanChangeGame() => IsConnected && IsPlaying && !IsStopping && _currentSession?.Purpose == "game" && SelectedGame?.EmulatorId is { Length: > 0 } && SelectedGame.Id != _currentSession.GameId;
    [RelayCommand(CanExecute = nameof(CanChangeGame))]
    private void ChangeGame()
    {
        _pendingGame = SelectedGame;
        _pendingSessionId = _currentSession!.Id;
        SwitchDescription = $"End the current game and play {_pendingGame!.Title}? Save in the game first; unsaved progress may be lost.";
        ShowSwitchConfirmation = true;
    }
    [RelayCommand]
    private void CancelChangeGame()
    {
        _pendingGame = null; _pendingSessionId = null; ShowSwitchConfirmation = false;
        // A submitted close request cannot be undone. Cancel only the next launch.
        if (IsStopping) SessionDescription = "Next launch canceled. The current exit request is still pending.";
    }
    [RelayCommand]
    private async Task ConfirmChangeGameAsync()
    {
        if (!ShowSwitchConfirmation || _pendingGame is null || _pendingSessionId is null) return;
        ShowSwitchConfirmation = false;
        await RequestStopAsync(_pendingSessionId);
    }
    [RelayCommand]
    private void ResumeGame() => ResumeRequested?.Invoke(this, EventArgs.Empty);

    private async Task RequestStopAsync(string expectedId)
    {
        IsStopping = true;
        _stopRequestedAt = DateTimeOffset.UtcNow;
        try
        {
            var connection = await _session.ConnectAsync(_lifetime.Token);
            await connection.CallAsync<System.Text.Json.JsonElement>("session.stop", new { force = false, expectedSessionId = expectedId }, _lifetime.Token);
            SessionDescription = "Waiting for the game to close. If prompted, confirm exit in the emulator using Back to game.";
        }
        catch (Exception e) when (IsUserFacingFailure(e)) { ClearTransition(); ShowError(e); }
    }
    private void ClearTransition()
    {
        _pendingGame = null; _pendingSessionId = null; ShowSwitchConfirmation = false;
        IsStopping = false; _stopRequestedAt = null;
    }
    [RelayCommand(CanExecute = nameof(CanPlay))]
    private async Task PlayAsync()
    {
        try
        {
            var connection = await _session.ConnectAsync(_lifetime.Token);
            var state = await connection.CallAsync<PlaySession>("session.launch", new { id = SelectedGame!.Id }, _lifetime.Token);
            SetSession(state); SessionDescription = "Game running"; HasError = false;
        }
        catch (Exception e) when (IsUserFacingFailure(e)) { ShowError(e); }
    }
    [RelayCommand(CanExecute = nameof(CanStop))]
    private async Task StopGameAsync()
    {
        CancelChangeGame();
        if (_currentSession is not null) await RequestStopAsync(_currentSession.Id);
    }
    private void SetSession(PlaySession state)
    {
        _currentSession = state;
        IsPlaying = state.State == "running";
        RunningGenreId = IsPlaying ? _allGames.FirstOrDefault(g => g.Id == state.GameId)?.GenreId : null;
        OnPropertyChanged(nameof(RunningProcessId));
        ChangeGameCommand.NotifyCanExecuteChanged();
    }
    [RelayCommand]
    private async Task PollSessionAsync()
    {
        if (!IsConnected) return;
        try
        {
            var connection = await _session.ConnectAsync(_lifetime.Token);
            var state = await connection.CallAsync<PlaySession>("session.status", cancellationToken: _lifetime.Token);
            var wasPlaying = IsPlaying;
            SetSession(state);
            if (_pendingGame is not null && _pendingSessionId != state.Id)
            {
                ClearTransition();
                ShowError(new IOException("The session changed. Select the next game again."));
            }
            if (IsStopping && state.State == "exited")
            {
                var next = state.ExitCode == 0 ? _pendingGame : null;
                ClearTransition();
                if (next is not null)
                {
                    var launched = await connection.CallAsync<PlaySession>("session.launch", new { id = next.Id }, _lifetime.Token);
                    SetSession(launched); SessionDescription = $"Playing {next.Title}"; HasError = false;
                    return;
                }
                if (state.ExitCode != 0) ShowError(new IOException("The emulator exited unexpectedly. Select Play when ready; no replacement was launched."));
            }
            else if (IsStopping && (state.State != "running" || DateTimeOffset.UtcNow - _stopRequestedAt > TimeSpan.FromSeconds(60)))
            {
                ClearTransition();
                ShowError(new IOException("Game change stopped waiting. Check the emulator's exit prompt, then select a game again. No forced exit was used."));
            }
            if (!IsStopping) SessionDescription = state.State switch {
                "running" when state.Purpose == "configure" => "Emulator settings open",
                "running" when state.Purpose == "firmware" => "Firmware installer open",
                "running" => "Game running", "interrupted" => "Session needs operator attention", _ => "No game running" };
            if (wasPlaying && !IsPlaying) GameEnded?.Invoke(this, EventArgs.Empty);
        }
        catch (Exception e) when (IsUserFacingFailure(e)) { ClearTransition(); IsConnected = false; ShowError(e); }
    }
    private static bool IsUserFacingFailure(Exception e) => e is IOException or InvalidDataException or TimeoutException or OperationCanceledException or CoreRpcException or System.Text.Json.JsonException or System.ComponentModel.Win32Exception;
    private void ShowError(Exception e) { HasError = true; Error = e is OperationCanceledException ? "The action was interrupted. Refresh to reconnect." : e.Message; }

    public async ValueTask DisposeAsync()
    {
        await _lifetime.CancelAsync();
        if (RefreshCommand.ExecutionTask is { } pending) await pending;
        ClearTransition();
        foreach (var command in new[] { PlayCommand, StopGameCommand, PollSessionCommand, ConfigureStorageCommand, ConfirmChangeGameCommand })
            if (command.ExecutionTask is { } work) await work;
        await _session.DisposeAsync();
        _lifetime.Dispose();
    }
}
