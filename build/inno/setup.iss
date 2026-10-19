; Inno Setup script for WhatsApp Manager (per-user install, no admin rights).
; Built by build.ps1:  ISCC /DAppVersion=1.2.3 /DNumericVersion=1.2.3.0 /DSourceDir=dist\app /DOutputDir=dist setup.iss

#ifndef AppVersion
  #define AppVersion "0.0.0"
#endif
#ifndef NumericVersion
  #define NumericVersion "0.0.0.0"
#endif
#ifndef SourceDir
  #define SourceDir "..\..\dist\app"
#endif
#ifndef OutputDir
  #define OutputDir "..\..\dist"
#endif

#define AppName "WhatsApp Manager"
#define AppExe "WhatsAppManager.exe"
#define AppUserModelID "WhatsAppManager.Desktop"

[Setup]
AppId={{5B60F159-309B-4422-85FD-A7899358F6D2}
AppName={#AppName}
AppVersion={#AppVersion}
AppVerName={#AppName} {#AppVersion}
AppPublisher=WhatsApp Manager contributors
VersionInfoVersion={#NumericVersion}
VersionInfoProductName={#AppName}
DefaultDirName={userpf}\WhatsAppManager
DefaultGroupName={#AppName}
DisableProgramGroupPage=yes
DisableDirPage=yes
DisableReadyPage=yes
PrivilegesRequired=lowest
PrivilegesRequiredOverridesAllowed=
ArchitecturesAllowed=x64compatible
ArchitecturesInstallIn64BitMode=x64compatible
MinVersion=10.0.17763
OutputDir={#OutputDir}
OutputBaseFilename=WhatsAppManager-Setup-{#AppVersion}
SetupIconFile=..\..\assets\icons\app.ico
UninstallDisplayIcon={app}\{#AppExe}
UninstallDisplayName={#AppName}
Compression=lzma2/ultra64
SolidCompression=yes
WizardStyle=modern
CloseApplications=force
RestartApplications=no
ShowLanguageDialog=auto
LicenseFile=..\..\LICENSE

[Languages]
Name: "arabic"; MessagesFile: "compiler:Languages\Arabic.isl"
Name: "english"; MessagesFile: "compiler:Default.isl"

[CustomMessages]
arabic.DesktopIcon=إنشاء اختصار على سطح المكتب
english.DesktopIcon=Create a desktop shortcut
arabic.LaunchApp=تشغيل WhatsApp Manager
english.LaunchApp=Launch WhatsApp Manager
arabic.RemoveData=هل تريد حذف الإعدادات والقواعد وجلسة واتساب المحفوظة أيضاً؟%n%nاختر «لا» إذا كنت ستعيد التثبيت لاحقاً.
english.RemoveData=Also delete settings, rules and the saved WhatsApp session?%n%nChoose "No" if you plan to reinstall.
arabic.NoWebView=لم يتم العثور على Microsoft Edge WebView2 Runtime. سيعمل التطبيق داخل المتصفح بدلاً من نافذته الخاصة. يمكنك تثبيته لاحقاً من موقع مايكروسوفت.
english.NoWebView=Microsoft Edge WebView2 Runtime was not found. The app will open in your browser instead of its own window. You can install WebView2 later from Microsoft.

[Tasks]
Name: "desktopicon"; Description: "{cm:DesktopIcon}"; Flags: unchecked

[Files]
Source: "{#SourceDir}\*"; DestDir: "{app}"; Flags: ignoreversion recursesubdirs createallsubdirs

[Icons]
Name: "{userprograms}\{#AppName}"; Filename: "{app}\{#AppExe}"; AppUserModelID: "{#AppUserModelID}"
Name: "{userdesktop}\{#AppName}"; Filename: "{app}\{#AppExe}"; Tasks: desktopicon

[Registry]
; The app manages its own autostart entry; make sure uninstall removes it.
Root: HKCU; Subkey: "Software\Microsoft\Windows\CurrentVersion\Run"; ValueName: "WhatsAppManager"; ValueType: none; Flags: uninsdeletevalue dontcreatekey

[Run]
Filename: "{app}\{#AppExe}"; Description: "{cm:LaunchApp}"; Flags: nowait postinstall skipifsilent

[Code]
procedure KillApp();
var
  ResultCode: Integer;
begin
  Exec(ExpandConstant('{sys}\taskkill.exe'), '/F /T /IM {#AppExe}', '', SW_HIDE, ewWaitUntilTerminated, ResultCode);
  Sleep(800);
end;

function WebView2Installed(): Boolean;
var
  V: String;
begin
  Result :=
    (RegQueryStringValue(HKLM, 'SOFTWARE\WOW6432Node\Microsoft\EdgeUpdate\Clients\{F3017226-FE2A-4295-8BDF-00C3A9A7E4C5}', 'pv', V) and (V <> '') and (V <> '0.0.0.0')) or
    (RegQueryStringValue(HKLM, 'SOFTWARE\Microsoft\EdgeUpdate\Clients\{F3017226-FE2A-4295-8BDF-00C3A9A7E4C5}', 'pv', V) and (V <> '') and (V <> '0.0.0.0')) or
    (RegQueryStringValue(HKCU, 'Software\Microsoft\EdgeUpdate\Clients\{F3017226-FE2A-4295-8BDF-00C3A9A7E4C5}', 'pv', V) and (V <> '') and (V <> '0.0.0.0'));
end;

function PrepareToInstall(var NeedsRestart: Boolean): String;
begin
  KillApp();
  Result := '';
end;

procedure CurStepChanged(CurStep: TSetupStep);
begin
  if (CurStep = ssPostInstall) and (not WizardSilent()) and (not WebView2Installed()) then
    MsgBox(CustomMessage('NoWebView'), mbInformation, MB_OK);
end;

function InitializeUninstall(): Boolean;
begin
  KillApp();
  Result := True;
end;

procedure CurUninstallStepChanged(CurUninstallStep: TUninstallStep);
var
  DataDir: String;
begin
  if CurUninstallStep = usPostUninstall then
  begin
    DataDir := ExpandConstant('{localappdata}\WhatsAppManager');
    if DirExists(DataDir) and (not UninstallSilent()) then
      if MsgBox(CustomMessage('RemoveData'), mbConfirmation, MB_YESNO or MB_DEFBUTTON2) = IDYES then
        DelTree(DataDir, True, True, True);
  end;
end;
