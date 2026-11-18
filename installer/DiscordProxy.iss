; Inno Setup 6 script. Build:  iscc /DAppVersion=1.0.0 installer\DiscordProxy.iss   (needs dist\DiscordProxy.exe)
#ifndef AppVersion
  #define AppVersion "0.0.0"
#endif

[Setup]
AppId={{8D3B2388-FA74-4B9C-81D3-620C9442B1EF}
AppName=Discord Proxy
AppVersion={#AppVersion}
AppPublisher=Unofficial (not affiliated with Discord Inc.)
DefaultDirName={localappdata}\Programs\Discord Proxy
DisableProgramGroupPage=yes
PrivilegesRequired=lowest
OutputDir=..\dist
OutputBaseFilename=DiscordProxy-Setup-{#AppVersion}
SetupIconFile=..\assets\icon.ico
UninstallDisplayIcon={app}\DiscordProxy.exe
Compression=lzma2
SolidCompression=yes
WizardStyle=modern
ArchitecturesAllowed=x64compatible
ArchitecturesInstallIn64BitMode=x64compatible
CloseApplications=yes
RestartApplications=no

[Languages]
Name: "english"; MessagesFile: "compiler:Default.isl"

[Tasks]
Name: "desktopicon"; Description: "Create a &desktop shortcut"; GroupDescription: "Shortcuts:"

[Files]
Source: "..\dist\DiscordProxy.exe"; DestDir: "{app}"; Flags: ignoreversion
Source: "..\README.md"; DestDir: "{app}"; Flags: ignoreversion
Source: "..\LICENSE"; DestDir: "{app}"; Flags: ignoreversion

[Icons]
Name: "{autoprograms}\Discord Proxy"; Filename: "{app}\DiscordProxy.exe"
Name: "{autodesktop}\Discord Proxy"; Filename: "{app}\DiscordProxy.exe"; Tasks: desktopicon

[Run]
Filename: "{app}\DiscordProxy.exe"; Description: "Open Discord through the proxy now"; Flags: nowait postinstall skipifsilent

[UninstallRun]
Filename: "{sys}\taskkill.exe"; Parameters: "/F /IM DiscordProxy.exe"; Flags: runhidden; RunOnceId: "StopDiscordProxy"

[UninstallDelete]
Type: filesandordirs; Name: "{localappdata}\DiscordProxy"

[Code]
// An older copy of the background proxy may still be running: stop it so its file can be replaced.
function PrepareToInstall(var NeedsRestart: Boolean): String;
var
  ResultCode: Integer;
begin
  Exec(ExpandConstant('{sys}\taskkill.exe'), '/F /IM DiscordProxy.exe', '', SW_HIDE, ewWaitUntilTerminated, ResultCode);
  Result := '';
end;
