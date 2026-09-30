package tray

import (
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"unicode/utf16"
)

var pinTokenRegex = regexp.MustCompile(`PIN_TOKEN:(\d{6})`)

// psQuote escapes a string for safe embedding into a PowerShell single-quoted literal.
func psQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

// encodePowerShellCommand converts a PowerShell script to base64 UTF-16LE for -EncodedCommand.
func encodePowerShellCommand(script string) string {
	runes := utf16.Encode([]rune(script))
	bytes := make([]byte, len(runes)*2)
	for i, r := range runes {
		bytes[i*2] = byte(r)
		bytes[i*2+1] = byte(r >> 8)
	}
	return base64.StdEncoding.EncodeToString(bytes)
}

// showPinForm displays a native Windows Forms dialog with PIN and Confirm PIN fields.
func showPinForm(title, header, desc, saveBtn, cancelBtn string) (string, error) {
	exePath, err := os.Executable()
	if err != nil {
		exePath = "PC-Remote.exe"
	}
	exeDir := filepath.Dir(exePath)
	iconPngPath := filepath.Join(exeDir, "icon.png")

	script := fmt.Sprintf(`
$ProgressPreference = 'SilentlyContinue'
Add-Type -AssemblyName System.Windows.Forms
Add-Type -AssemblyName System.Drawing
[System.Windows.Forms.Application]::EnableVisualStyles()

$form = New-Object System.Windows.Forms.Form
$form.Text = %s
$form.Size = New-Object System.Drawing.Size(440, 440)
$form.StartPosition = "CenterScreen"
$form.FormBorderStyle = "FixedDialog"
$form.MaximizeBox = $false
$form.MinimizeBox = $false
$form.TopMost = $true
$form.BackColor = [System.Drawing.Color]::FromArgb(9, 9, 11)
$form.ForeColor = [System.Drawing.Color]::FromArgb(244, 244, 245)

# Win32 Native Helpers for AppUserModelID, dark title bar, and foreground focus
try {
    $sig = @'
[DllImport("shell32.dll", CharSet = CharSet.Unicode)] public static extern int SetCurrentProcessExplicitAppUserModelID(string AppID);
[DllImport("user32.dll")] public static extern bool SetForegroundWindow(IntPtr hWnd);
[DllImport("user32.dll")] public static extern bool BringWindowToTop(IntPtr hWnd);
[DllImport("user32.dll")] public static extern bool AllowSetForegroundWindow(int dwProcessId);
[DllImport("dwmapi.dll")] public static extern int DwmSetWindowAttribute(IntPtr hwnd, int attr, ref int val, int size);
'@
    Add-Type -MemberDefinition $sig -Name "NativeHelper" -Namespace "PCRemote" -ErrorAction SilentlyContinue
    [void][PCRemote.NativeHelper]::SetCurrentProcessExplicitAppUserModelID("PCRemote.App")
    $dark = 1
    [void][PCRemote.NativeHelper]::DwmSetWindowAttribute($form.Handle, 20, [ref]$dark, 4)
    [void][PCRemote.NativeHelper]::DwmSetWindowAttribute($form.Handle, 19, [ref]$dark, 4)
} catch {}

# Assign custom icon from icon.png or LaptopControl.exe
try {
    $iconPath = %s
    $exePath = %s
    if (Test-Path $iconPath) {
        $bmp = [System.Drawing.Bitmap]::FromFile($iconPath)
        $hIco = $bmp.GetHicon()
        $form.Icon = [System.Drawing.Icon]::FromHandle($hIco)
    } elseif (Test-Path $exePath) {
        $form.Icon = [System.Drawing.Icon]::ExtractAssociatedIcon($exePath)
    }
} catch {}

# Header
$lblHeader = New-Object System.Windows.Forms.Label
$lblHeader.Text = %s
$lblHeader.Font = New-Object System.Drawing.Font("Segoe UI", 13, [System.Drawing.FontStyle]::Bold)
$lblHeader.ForeColor = [System.Drawing.Color]::FromArgb(244, 244, 245)
$lblHeader.Location = New-Object System.Drawing.Point(28, 20)
$lblHeader.Size = New-Object System.Drawing.Size(370, 30)
$form.Controls.Add($lblHeader)

# Description - 55px height so multi-line text is never cut off
$lblDesc = New-Object System.Windows.Forms.Label
$lblDesc.Text = %s
$lblDesc.Font = New-Object System.Drawing.Font("Segoe UI", 9.5, [System.Drawing.FontStyle]::Regular)
$lblDesc.ForeColor = [System.Drawing.Color]::FromArgb(161, 161, 170)
$lblDesc.Location = New-Object System.Drawing.Point(28, 54)
$lblDesc.Size = New-Object System.Drawing.Size(370, 55)
$form.Controls.Add($lblDesc)

# Label PIN
$lblPin = New-Object System.Windows.Forms.Label
$lblPin.Text = "ENTER 6-DIGIT PIN"
$lblPin.Font = New-Object System.Drawing.Font("Segoe UI", 8, [System.Drawing.FontStyle]::Bold)
$lblPin.ForeColor = [System.Drawing.Color]::FromArgb(161, 161, 170)
$lblPin.Location = New-Object System.Drawing.Point(28, 120)
$lblPin.Size = New-Object System.Drawing.Size(370, 18)
$form.Controls.Add($lblPin)

# Input PIN - UseSystemPasswordChar for vertically centered native bullet dots
$txtPin = New-Object System.Windows.Forms.TextBox
$txtPin.Font = New-Object System.Drawing.Font("Segoe UI", 12, [System.Drawing.FontStyle]::Bold)
$txtPin.BackColor = [System.Drawing.Color]::FromArgb(24, 24, 27)
$txtPin.ForeColor = [System.Drawing.Color]::FromArgb(244, 244, 245)
$txtPin.BorderStyle = [System.Windows.Forms.BorderStyle]::FixedSingle
$txtPin.Location = New-Object System.Drawing.Point(28, 142)
$txtPin.Size = New-Object System.Drawing.Size(368, 32)
$txtPin.MaxLength = 6
$txtPin.UseSystemPasswordChar = $true
$txtPin.TextAlign = "Center"
$form.Controls.Add($txtPin)

# Label Confirm
$lblConfirm = New-Object System.Windows.Forms.Label
$lblConfirm.Text = "CONFIRM 6-DIGIT PIN"
$lblConfirm.Font = New-Object System.Drawing.Font("Segoe UI", 8, [System.Drawing.FontStyle]::Bold)
$lblConfirm.ForeColor = [System.Drawing.Color]::FromArgb(161, 161, 170)
$lblConfirm.Location = New-Object System.Drawing.Point(28, 188)
$lblConfirm.Size = New-Object System.Drawing.Size(370, 18)
$form.Controls.Add($lblConfirm)

# Input Confirm - UseSystemPasswordChar for vertically centered native bullet dots
$txtConfirm = New-Object System.Windows.Forms.TextBox
$txtConfirm.Font = New-Object System.Drawing.Font("Segoe UI", 12, [System.Drawing.FontStyle]::Bold)
$txtConfirm.BackColor = [System.Drawing.Color]::FromArgb(24, 24, 27)
$txtConfirm.ForeColor = [System.Drawing.Color]::FromArgb(244, 244, 245)
$txtConfirm.BorderStyle = [System.Windows.Forms.BorderStyle]::FixedSingle
$txtConfirm.Location = New-Object System.Drawing.Point(28, 210)
$txtConfirm.Size = New-Object System.Drawing.Size(368, 32)
$txtConfirm.MaxLength = 6
$txtConfirm.UseSystemPasswordChar = $true
$txtConfirm.TextAlign = "Center"
$form.Controls.Add($txtConfirm)

# Show PIN Checkbox
$chkShow = New-Object System.Windows.Forms.CheckBox
$chkShow.Text = "Show PIN digits"
$chkShow.Font = New-Object System.Drawing.Font("Segoe UI", 8.5)
$chkShow.ForeColor = [System.Drawing.Color]::FromArgb(161, 161, 170)
$chkShow.BackColor = [System.Drawing.Color]::FromArgb(9, 9, 11)
$chkShow.FlatStyle = [System.Windows.Forms.FlatStyle]::Flat
$chkShow.Location = New-Object System.Drawing.Point(30, 254)
$chkShow.Size = New-Object System.Drawing.Size(160, 22)
$chkShow.Add_CheckedChanged({
    $txtPin.UseSystemPasswordChar = -not $chkShow.Checked
    $txtConfirm.UseSystemPasswordChar = -not $chkShow.Checked
})
$form.Controls.Add($chkShow)

# Error Label
$lblError = New-Object System.Windows.Forms.Label
$lblError.Font = New-Object System.Drawing.Font("Segoe UI", 8.5, [System.Drawing.FontStyle]::Regular)
$lblError.ForeColor = [System.Drawing.Color]::FromArgb(239, 68, 68)
$lblError.BackColor = [System.Drawing.Color]::FromArgb(9, 9, 11)
$lblError.Location = New-Object System.Drawing.Point(28, 282)
$lblError.Size = New-Object System.Drawing.Size(368, 22)
$lblError.Text = ""
$form.Controls.Add($lblError)

# Save Button (Emerald Green Accent, UseMnemonic = false so & displays literally)
$btnSave = New-Object System.Windows.Forms.Button
$btnSave.Text = %s
$btnSave.UseMnemonic = $false
$btnSave.Font = New-Object System.Drawing.Font("Segoe UI", 9.5, [System.Drawing.FontStyle]::Bold)
$btnSave.BackColor = [System.Drawing.Color]::FromArgb(34, 197, 94) # Emerald #22c55e
$btnSave.ForeColor = [System.Drawing.Color]::FromArgb(9, 9, 11)   # Dark contrast text
$btnSave.FlatStyle = [System.Windows.Forms.FlatStyle]::Flat
$btnSave.FlatAppearance.BorderSize = 0
$btnSave.Cursor = [System.Windows.Forms.Cursors]::Hand
$btnSave.Location = New-Object System.Drawing.Point(208, 318)
$btnSave.Size = New-Object System.Drawing.Size(188, 38)

# Cancel Button (Subtle Dark)
$btnCancel = New-Object System.Windows.Forms.Button
$btnCancel.Text = %s
$btnCancel.UseMnemonic = $false
$btnCancel.Font = New-Object System.Drawing.Font("Segoe UI", 9, [System.Drawing.FontStyle]::Regular)
$btnCancel.BackColor = [System.Drawing.Color]::FromArgb(24, 24, 27) # #18181b
$btnCancel.ForeColor = [System.Drawing.Color]::FromArgb(161, 161, 170)
$btnCancel.FlatStyle = [System.Windows.Forms.FlatStyle]::Flat
$btnCancel.FlatAppearance.BorderColor = [System.Drawing.Color]::FromArgb(39, 39, 42)
$btnCancel.FlatAppearance.BorderSize = 1
$btnCancel.Cursor = [System.Windows.Forms.Cursors]::Hand
$btnCancel.Location = New-Object System.Drawing.Point(98, 318)
$btnCancel.Size = New-Object System.Drawing.Size(100, 38)

$btnSave.Add_Click({
    $p = $txtPin.Text.Trim()
    $c = $txtConfirm.Text.Trim()
    if ($p.Length -ne 6 -or -not ($p -match '^\d{6}$')) {
        $lblError.Text = "PIN must be exactly 6 numeric digits (0-9)."
        return
    }
    if ($p -ne $c) {
        $lblError.Text = "PINs do not match. Please re-enter."
        return
    }
    try {
        [PCRemote.NativeHelper]::AllowSetForegroundWindow(-1)
    } catch {}
    [Console]::Out.WriteLine("PIN_TOKEN:" + $p)
    $form.DialogResult = [System.Windows.Forms.DialogResult]::OK
    $form.Close()
})

$btnCancel.Add_Click({
    $form.DialogResult = [System.Windows.Forms.DialogResult]::Cancel
    $form.Close()
})

$form.Controls.Add($btnSave)
$form.Controls.Add($btnCancel)
$form.AcceptButton = $btnSave
$form.CancelButton = $btnCancel
$form.Add_Shown({
    try {
        [PCRemote.NativeHelper]::BringWindowToTop($form.Handle)
        [PCRemote.NativeHelper]::SetForegroundWindow($form.Handle)
    } catch {}
    $form.Activate()
    $form.BringToFront()
    $txtPin.Focus()
    $txtPin.Select()
})

$null = $form.ShowDialog()
`, psQuote(title), psQuote(iconPngPath), psQuote(exePath), psQuote(header), psQuote(desc), psQuote(saveBtn), psQuote(cancelBtn))

	// Allow newly created process to take foreground window focus
	procAllowSetForegroundWindow.Call(ASFW_ANY)

	encoded := encodePowerShellCommand(script)
	cmd := exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-EncodedCommand", encoded)
	const CREATE_NO_WINDOW = 0x08000000
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: CREATE_NO_WINDOW,
	}

	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", err
	}

	matches := pinTokenRegex.FindStringSubmatch(string(out))
	if len(matches) < 2 {
		return "", fmt.Errorf("no PIN token received (setup cancelled)")
	}

	pin := matches[1]
	return pin, nil
}

// PromptInitialPinSetup launches the first-time setup dialog to establish a 6-digit PIN.
// Returns the 6-digit PIN and true on success, or empty and false if cancelled.
func PromptInitialPinSetup(deviceName string) (string, bool) {
	title := "PC Remote - Initial Setup"
	header := "Create Master PIN (Required)"
	desc := fmt.Sprintf("Welcome to PC Remote on %s!\nPlease create a 6-digit Master PIN to secure remote access from your phone.", deviceName)
	saveBtn := "Confirm & Continue"
	cancelBtn := "Exit Setup"

	pin, err := showPinForm(title, header, desc, saveBtn, cancelBtn)
	if err != nil || len(pin) != 6 || !isAllDigits(pin) {
		// User closed or clicked Exit Setup -> exit cleanly without modal popup
		return "", false
	}

	// Success! Return PIN directly so app transitions smoothly to QR pairing window
	return pin, true
}

