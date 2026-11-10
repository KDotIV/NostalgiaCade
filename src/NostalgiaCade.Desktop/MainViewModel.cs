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
    public ObservableCollection<Genre> Genres { get; } = [];
    public ObservableCollection<LocalGame> Games { get; } = [];
    public event EventHandler? GameEnded;

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
    private bool CanPlay() => IsConnected && !SetupRequired && !IsPlaying && SelectedGame?.EmulatorId is { Length: > 0 };
    private bool CanStop() => IsPlaying;
    [RelayCommand(CanExecute = nameof(CanPlay))]
    private async Task PlayAsync()
    {
        try
        {
            var connection = await _session.ConnectAsync(_lifetime.Token);
            var state = await connection.CallAsync<PlaySession>("session.launch", new { id = SelectedGame!.Id }, _lifetime.Token);
            IsPlaying = state.State == "running"; SessionDescription = "Game running"; HasError = false;
        }
        catch (Exception e) when (IsUserFacingFailure(e)) { ShowError(e); }
    }
    [RelayCommand(CanExecute = nameof(CanStop))]
    private async Task StopGameAsync()
    {
        try
        {
            var connection = await _session.ConnectAsync(_lifetime.Token);
            await connection.CallAsync<System.Text.Json.JsonElement>("session.stop", new { force = false }, _lifetime.Token);
            SessionDescription = "Waiting for the game to close…";
        }
        catch (Exception e) when (IsUserFacingFailure(e)) { ShowError(e); }
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
            IsPlaying = state.State == "running";
            SessionDescription = state.State switch { "running" => "Game running", "interrupted" => "Session needs operator attention", _ => "No game running" };
            if (wasPlaying && !IsPlaying) GameEnded?.Invoke(this, EventArgs.Empty);
        }
        catch (Exception e) when (IsUserFacingFailure(e)) { IsConnected = false; ShowError(e); }
    }
    private static bool IsUserFacingFailure(Exception e) => e is IOException or InvalidDataException or TimeoutException or OperationCanceledException or CoreRpcException or System.Text.Json.JsonException or System.ComponentModel.Win32Exception;
    private void ShowError(Exception e) { HasError = true; Error = e is OperationCanceledException ? "The action was interrupted. Refresh to reconnect." : e.Message; }

    public async ValueTask DisposeAsync()
    {
        await _lifetime.CancelAsync();
        if (RefreshCommand.ExecutionTask is { } pending) await pending;
        foreach (var command in new[] { PlayCommand, StopGameCommand, PollSessionCommand, ConfigureStorageCommand })
            if (command.ExecutionTask is { } work) await work;
        await _session.DisposeAsync();
        _lifetime.Dispose();
    }
}
