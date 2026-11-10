using System.Text.Json;
using System.Text.Json.Serialization;

namespace NostalgiaCade.Desktop;

// Physical inputs share one action; profiles are selected from the running game's
// genre, never the genre currently being browsed in the library.
public sealed class CabinetControls
{
    public CabinetBinding Default { get; init; } = new();
    public Dictionary<string, CabinetBinding> Genres { get; init; } = [];

    public CabinetBinding ForGenre(string? genre) => genre is not null && Genres.TryGetValue(genre, out var binding) ? binding : Default;

    public static CabinetControls Load(string dataDirectory)
    {
        var path = Path.Combine(dataDirectory, "cabinet-controls.json");
        if (!File.Exists(path)) return new();
        if (new FileInfo(path).Length > 64 * 1024) throw new InvalidDataException("Cabinet controls file is too large.");
        var controls = JsonSerializer.Deserialize<CabinetControls>(File.ReadAllText(path), new JsonSerializerOptions {
            PropertyNameCaseInsensitive = true, UnmappedMemberHandling = JsonUnmappedMemberHandling.Disallow
        }) ?? throw new InvalidDataException("Cabinet controls file is empty.");
        if (controls.Default is null || controls.Genres is null || controls.Genres.Count > 100)
            throw new InvalidDataException("Cabinet controls must define valid defaults and genre overrides.");
        controls.Default.Validate();
        foreach (var (genre, binding) in controls.Genres)
        {
            if (string.IsNullOrWhiteSpace(genre) || binding is null) throw new InvalidDataException("Invalid genre control mapping.");
            binding.Validate();
        }
        return controls;
    }
}

public sealed record CabinetBinding
{
    public string Keyboard { get; init; } = "Ctrl+Alt+Home";
    public string[] ControllerButtons { get; init; } = ["Back", "Start"];
    public int HoldMilliseconds { get; init; } = 1500;

    private static readonly Dictionary<string, ushort> Buttons = new(StringComparer.OrdinalIgnoreCase) {
        ["DpadUp"] = 1, ["DpadDown"] = 2, ["DpadLeft"] = 4, ["DpadRight"] = 8,
        ["Start"] = 0x10, ["Back"] = 0x20, ["LeftThumb"] = 0x40, ["RightThumb"] = 0x80,
        ["LeftShoulder"] = 0x100, ["RightShoulder"] = 0x200,
        ["A"] = 0x1000, ["B"] = 0x2000, ["X"] = 0x4000, ["Y"] = 0x8000
    };
    public ushort ControllerMask => (ushort)ControllerButtons.Aggregate(0, (mask, button) => mask | Buttons[button]);
    public string Hint => $"{Keyboard} · Controller: hold {string.Join(" + ", ControllerButtons)} for {HoldMilliseconds / 1000.0:0.#}s";

    public (uint Modifiers, uint Key) Hotkey()
    {
        if (string.IsNullOrWhiteSpace(Keyboard)) throw new InvalidDataException("A keyboard return binding is required.");
        var parts = Keyboard.Split('+', StringSplitOptions.TrimEntries);
        uint modifiers = 0;
        foreach (var modifier in parts[..^1]) modifiers |= modifier.ToUpperInvariant() switch {
            "CTRL" => 2u, "ALT" => 1u, "SHIFT" => 4u,
            _ => throw new InvalidDataException("Return shortcut modifiers must be Ctrl, Alt or Shift.")
        };
        var key = parts[^1].ToUpperInvariant();
        uint code = key switch {
            "HOME" => 0x24, "END" => 0x23, "PAGEUP" => 0x21, "PAGEDOWN" => 0x22,
            _ when key.Length == 1 && char.IsAsciiLetterOrDigit(key[0]) => key[0],
            _ when key.StartsWith('F') && int.TryParse(key[1..], out var f) && f is >= 1 and <= 24 && f != 12 => (uint)(0x6F + f),
            _ => throw new InvalidDataException("Unsupported return shortcut key. Use Home, End, PageUp, PageDown, a letter/digit or F1–F24 (except F12).")
        };
        if (modifiers == 0 && code < 0x7C) throw new InvalidDataException("Use a modifier chord or a dedicated F13–F24 cabinet key.");
        return (modifiers | 0x4000, code); // MOD_NOREPEAT
    }

    public void Validate()
    {
        _ = Hotkey();
        if (ControllerButtons is null || ControllerButtons.Distinct(StringComparer.OrdinalIgnoreCase).Count() < 2 ||
            ControllerButtons.Any(b => b is null || !Buttons.ContainsKey(b)) || HoldMilliseconds is < 750 or > 5000)
            throw new InvalidDataException("Use at least two distinct XInput buttons and a hold of 750–5000 milliseconds.");
    }
}

public sealed class HoldGesture
{
    private long? _started;
    private bool _armed;
    private bool _fired;
    public bool Update(bool held, long now, int duration)
    {
        if (!held) { _armed = true; _started = null; _fired = false; return false; }
        if (!_armed || _fired) return false;
        _started ??= now;
        if (now - _started < duration) return false;
        _fired = true;
        return true;
    }
}
