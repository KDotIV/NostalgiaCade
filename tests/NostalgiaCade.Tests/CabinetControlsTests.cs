using NostalgiaCade.Desktop;
using Xunit;

namespace NostalgiaCade.Tests;

public class CabinetControlsTests
{
    [Fact]
    public void HoldRequiresReleaseThenDeliberateDurationAndDoesNotRepeat()
    {
        var gesture = new HoldGesture();
        Assert.False(gesture.Update(true, 0, 1500));
        Assert.False(gesture.Update(true, 2000, 1500)); // already held at attachment
        Assert.False(gesture.Update(false, 2100, 1500));
        Assert.False(gesture.Update(true, 2200, 1500));
        Assert.False(gesture.Update(true, 3600, 1500));
        Assert.True(gesture.Update(true, 3700, 1500));
        Assert.False(gesture.Update(true, 9000, 1500));
        Assert.False(gesture.Update(false, 9100, 1500));
        Assert.False(gesture.Update(true, 9200, 1500));
        Assert.True(gesture.Update(true, 10700, 1500));
    }

    [Fact]
    public void GenreBindingsFallBackAndRejectAccidentalGameplayButtons()
    {
        var controls = new CabinetControls { Genres = new() { ["racing"] = new() { Keyboard = "F13", HoldMilliseconds = 2000 } } };
        Assert.Equal("F13", controls.ForGenre("racing").Keyboard);
        Assert.Equal("Ctrl+Alt+Home", controls.ForGenre("fighting").Keyboard);
        Assert.Equal((ushort)0x30, controls.Default.ControllerMask);
        controls.ForGenre("racing").Validate();
        Assert.Throws<InvalidDataException>(() => new CabinetBinding { Keyboard = "A" }.Validate());
        Assert.Throws<InvalidDataException>(() => new CabinetBinding { Keyboard = "Ctrl+F12" }.Validate());
        Assert.Throws<InvalidDataException>(() => new CabinetBinding { ControllerButtons = ["A"] }.Validate());
        Assert.Throws<InvalidDataException>(() => new CabinetBinding { ControllerButtons = ["A", "a"] }.Validate());
        Assert.Throws<InvalidDataException>(() => new CabinetBinding { HoldMilliseconds = 1 }.Validate());
    }
}
