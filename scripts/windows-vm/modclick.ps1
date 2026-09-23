# Modifier-clicks for Windows-MCP, whose Click tool cannot hold Ctrl/Shift.
# Dot-source in the guest, then e.g.:
#   [In]::ModClick(450, 89, $false)   # Ctrl+click
#   [In]::ModClick(450, 89, $true)    # Ctrl+Shift+click
#   [In]::Hover(450, 89); ...screenshot...; [In]::CtrlUp()   # Ctrl-hover
# Coordinates are screen pixels, as reported by the Screenshot tool.
Add-Type @"
using System; using System.Runtime.InteropServices; using System.Threading;
public static class In {
  [DllImport("user32.dll")] public static extern bool SetCursorPos(int x, int y);
  [DllImport("user32.dll")] public static extern void keybd_event(byte k, byte s, uint f, UIntPtr e);
  [DllImport("user32.dll")] public static extern void mouse_event(uint f, int x, int y, uint d, UIntPtr e);
  const byte CTRL = 0x11, SHIFT = 0x10; const uint UP = 2, LDOWN = 2, LUP = 4;
  public static void Click(int x, int y) {
    SetCursorPos(x, y); Thread.Sleep(150);
    mouse_event(LDOWN, 0, 0, 0, UIntPtr.Zero); mouse_event(LUP, 0, 0, 0, UIntPtr.Zero);
  }
  public static void ModClick(int x, int y, bool shift) {
    SetCursorPos(x, y); Thread.Sleep(150);
    keybd_event(CTRL, 0, 0, UIntPtr.Zero); if (shift) keybd_event(SHIFT, 0, 0, UIntPtr.Zero);
    Thread.Sleep(250);  // let the link provider see the modifier on hover
    mouse_event(LDOWN, 0, 0, 0, UIntPtr.Zero); mouse_event(LUP, 0, 0, 0, UIntPtr.Zero);
    Thread.Sleep(150);
    if (shift) keybd_event(SHIFT, 0, UP, UIntPtr.Zero); keybd_event(CTRL, 0, UP, UIntPtr.Zero);
  }
  public static void Hover(int x, int y) { SetCursorPos(x, y); Thread.Sleep(150); keybd_event(CTRL, 0, 0, UIntPtr.Zero); }
  public static void CtrlUp() { keybd_event(CTRL, 0, UP, UIntPtr.Zero); }
}
"@
