#define MyAppName "PC Remote"
#define MyAppVersion "2.0.0"
#define MyAppPublisher "PC Remote Team"
#define MyAppExeName "PC-Remote.exe"

[Setup]
AppId={{D41A25FE-8438-4F42-8D1F-433E72387B4C}
AppName={#MyAppName}
AppVersion={#MyAppVersion}
AppPublisher={#MyAppPublisher}
DefaultDirName={autopf}\{#MyAppName}
DefaultGroupName={#MyAppName}
AllowNoIcons=yes
OutputDir=..\dist
OutputBaseFilename=PC-Remote-Setup
SetupIconFile=..\icon.ico
SolidCompression=yes
Compression=lzma2/ultra64
WizardStyle=modern
PrivilegesRequired=admin
UninstallDisplayIcon={app}\{#MyAppExeName}
CloseApplications=yes
RestartApplications=no

[Languages]
Name: "english"; MessagesFile: "compiler:Default.isl"

[Tasks]
Name: "desktopicon"; Description: "{cm:CreateDesktopIcon}"; GroupDescription: "{cm:AdditionalIcons}"

[Files]
Source: "..\{#MyAppExeName}"; DestDir: "{app}"; Flags: ignoreversion
Source: "..\icon.ico"; DestDir: "{app}"; Flags: ignoreversion
Source: "..\icon.png"; DestDir: "{app}"; Flags: ignoreversion

[Dirs]
Name: "{commonappdata}\{#MyAppName}"; Permissions: system-full admins-full users-readexec

[Icons]
Name: "{autoprograms}\{#MyAppName}"; Filename: "{app}\{#MyAppExeName}"; Parameters: "tray"
Name: "{autodesktop}\{#MyAppName}"; Filename: "{app}\{#MyAppExeName}"; Parameters: "tray"; Tasks: desktopicon

[Registry]
Root: HKLM; Subkey: "SOFTWARE\Microsoft\Windows\CurrentVersion\Run"; ValueType: string; ValueName: "PCRemoteTray"; ValueData: """{app}\{#MyAppExeName}"" tray -silent"; Flags: uninsdeletevalue

[Run]
; 1. Install Windows background service with SCM and automatic recovery
Filename: "{app}\{#MyAppExeName}"; Parameters: "install"; StatusMsg: "Registering PC Remote background service..."; Flags: runhidden waituntilterminated

; 2. Add narrow scoped Windows Firewall rule for LAN server on port 8765
Filename: "{sys}\netsh.exe"; Parameters: "advfirewall firewall add rule name=""PC Remote LAN Server"" dir=in action=allow program=""{app}\{#MyAppExeName}"" protocol=TCP localport=8765 profile=private"; StatusMsg: "Configuring local network firewall rule..."; Flags: runhidden waituntilterminated

; 3. Start background service immediately
Filename: "{app}\{#MyAppExeName}"; Parameters: "start"; StatusMsg: "Starting background service..."; Flags: runhidden waituntilterminated

; 4. Launch interactive tray in user session
Filename: "{app}\{#MyAppExeName}"; Parameters: "tray"; Description: "{cm:LaunchProgram,{#StringChange(MyAppName, '&', '&&')}}"; Flags: nowait postinstall skipifsilent

[UninstallRun]
; 1. Stop background service before uninstall
Filename: "{app}\{#MyAppExeName}"; Parameters: "stop"; Flags: runhidden waituntilterminated; RunOnceId: "StopPCRemoteService"

; 2. Unregister background service from SCM
Filename: "{app}\{#MyAppExeName}"; Parameters: "uninstall"; Flags: runhidden waituntilterminated; RunOnceId: "UninstallPCRemoteService"

; 3. Remove Windows Firewall rule
Filename: "{sys}\netsh.exe"; Parameters: "advfirewall firewall delete rule name=""PC Remote LAN Server"""; Flags: runhidden waituntilterminated; RunOnceId: "DeletePCRemoteFirewallRule"

[UninstallDelete]
Type: files; Name: "{app}\{#MyAppExeName}"
Type: files; Name: "{app}\icon.ico"
Type: files; Name: "{app}\icon.png"
Type: files; Name: "{app}\*.log"
Type: filesandordirs; Name: "{app}"

[Code]
// Gracefully terminate interactive tray process and stop service before upgrade or uninstall
function StopExistingService(): Boolean;
var
  ErrorCode: Integer;
begin
  Exec(ExpandConstant('{sys}\net.exe'), 'stop PCRemote', '', SW_HIDE, ewWaitUntilTerminated, ErrorCode);
  Exec(ExpandConstant('{sys}\taskkill.exe'), '/F /IM {#MyAppExeName}', '', SW_HIDE, ewWaitUntilTerminated, ErrorCode);
  Sleep(400);
  Result := True;
end;

function InitializeSetup(): Boolean;
begin
  StopExistingService();
  Result := True;
end;

function PrepareToInstall(var NeedsRestart: Boolean): String;
begin
  StopExistingService();
  Result := '';
end;

function InitializeUninstall(): Boolean;
begin
  StopExistingService();
  Result := True;
end;
