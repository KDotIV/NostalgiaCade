using System.Runtime.InteropServices;
using Avalonia.Controls;
using Avalonia.Threading;

namespace NostalgiaCade.Desktop;

internal enum CabinetMenuAction { Up, Down, PreviousGenre, NextGenre, Accept, Back }

internal sealed class CabinetInput : IDisposable
{
    private const int HotkeyId = 0x4E43;
    private readonly Window _window;
    private readonly Action _returnToLibrary;
    private readonly Func<int> _gameProcessId;
    private readonly Action<CabinetMenuAction> _menuAction;
    private readonly ushort[] _menuButtons = new ushort[4];
    private readonly bool[] _menuArmed = new bool[4];
    private readonly DispatcherTimer _timer = new() { Interval = TimeSpan.FromMilliseconds(100) };
    private readonly nint _handle;
    private CabinetBinding? _binding;
    private HoldGesture[] _gestures = [new(), new(), new(), new()];
    private bool _registered;
    private bool _controllerAvailable = true;

    public CabinetInput(Window window, Action returnToLibrary, Func<int> gameProcessId, Action<CabinetMenuAction> menuAction)
    {
        _window = window;
        _returnToLibrary = returnToLibrary;
        _gameProcessId = gameProcessId;
        _menuAction = menuAction;
        var platform = window.TryGetPlatformHandle();
        if (!OperatingSystem.IsWindows() || platform?.HandleDescriptor != "HWND") return;
        _handle = platform.Handle;
        Win32Properties.AddWndProcHookCallback(window, WindowMessage);
        _timer.Tick += (_, _) => PollControllers();
        _timer.Start();
    }

    public string Configure(CabinetBinding binding)
    {
        if (_binding == binding) return _registered ? binding.Hint : "Return shortcut unavailable; use Alt+Tab to open NCade.";
        binding.Validate();
        if (_registered) UnregisterHotKey(_handle, HotkeyId);
        _registered = false;
        _binding = binding;
        _gestures = [new(), new(), new(), new()];
        if (_handle != 0)
        {
            var hotkey = binding.Hotkey();
            _registered = RegisterHotKey(_handle, HotkeyId, hotkey.Modifiers, hotkey.Key);
        }
        return _registered ? binding.Hint : "Return shortcut unavailable; use Alt+Tab to open NCade.";
    }

    private nint WindowMessage(nint hwnd, uint message, nint wParam, nint lParam, ref bool handled)
    {
        if (message == 0x0312 && wParam == HotkeyId) { handled = true; Dispatcher.UIThread.Post(_returnToLibrary); }
        return 0;
    }

    private void PollControllers()
    {
        if (_binding is null || !_controllerAvailable) return;
        try
        {
            GetWindowThreadProcessId(GetForegroundWindow(), out var foregroundPid);
            var gameForeground = _gameProcessId() is > 0 && foregroundPid == _gameProcessId();
            for (uint slot = 0; slot < 4; slot++)
            {
                var connected = XInputGetState(slot, out var state) == 0;
                var held = gameForeground && connected && (state.Buttons & _binding.ControllerMask) == _binding.ControllerMask;
                if (_gestures[slot].Update(held, Environment.TickCount64, _binding.HoldMilliseconds)) _returnToLibrary();
                if (foregroundPid != Environment.ProcessId || !connected) { _menuArmed[slot] = false; _menuButtons[slot] = 0; continue; }
                if (!_menuArmed[slot]) { _menuArmed[slot] = state.Buttons == 0; continue; }
                var pressed = state.Buttons & ~_menuButtons[slot];
                _menuButtons[slot] = state.Buttons;
                // One navigation action per fresh press. Returning from a game
                // requires release before buttons can act on the library.
                CabinetMenuAction? action = (pressed & 0x2000) != 0 ? CabinetMenuAction.Back :
                    (pressed & 0x1000) != 0 ? CabinetMenuAction.Accept :
                    (pressed & 1) != 0 ? CabinetMenuAction.Up :
                    (pressed & 2) != 0 ? CabinetMenuAction.Down :
                    (pressed & 4) != 0 ? CabinetMenuAction.PreviousGenre :
                    (pressed & 8) != 0 ? CabinetMenuAction.NextGenre : null;
                if (action is { } selected)
                {
                    Array.Fill(_menuArmed, false); // No second controller can accept the newly opened confirmation while held.
                    _menuAction(selected);
                    return;
                }
            }
        }
        catch (Exception e) when (e is DllNotFoundException or EntryPointNotFoundException) { _controllerAvailable = false; }
    }

    public void Dispose()
    {
        _timer.Stop();
        if (_registered) UnregisterHotKey(_handle, HotkeyId);
        if (_handle != 0) Win32Properties.RemoveWndProcHookCallback(_window, WindowMessage);
    }

    [StructLayout(LayoutKind.Sequential)]
    private struct ControllerState
    {
        public uint Packet;
        public ushort Buttons;
        public byte LeftTrigger, RightTrigger;
        public short LeftX, LeftY, RightX, RightY;
    }
    [DllImport("user32.dll", SetLastError = true)]
    [return: MarshalAs(UnmanagedType.Bool)]
    private static extern bool RegisterHotKey(nint window, int id, uint modifiers, uint key);
    [DllImport("user32.dll")]
    [return: MarshalAs(UnmanagedType.Bool)]
    private static extern bool UnregisterHotKey(nint window, int id);
    [DllImport("user32.dll")]
    private static extern nint GetForegroundWindow();
    [DllImport("user32.dll")]
    private static extern uint GetWindowThreadProcessId(nint window, out uint processId);
    [DllImport("xinput1_4.dll")]
    private static extern uint XInputGetState(uint index, out ControllerState state);
}
