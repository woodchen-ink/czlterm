Unicode true

####
## Wails 每次 nsis 构建都会重新生成 wails_tools.nsh, 这里只放 czlterm 自己的安装逻辑。
## 手动调试: 先 `wails build -nsis` 生成 wails_tools.nsh, 再
## > makensis -DARG_WAILS_AMD64_BINARY=..\..\bin\czlterm.exe project.nsi
####

# 按用户安装到 %LOCALAPPDATA%\CZL\czlterm, 安装与更新全程不触发 UAC。
!define REQUEST_EXECUTION_LEVEL "user"

!include "wails_tools.nsh"

VIProductVersion "${INFO_PRODUCTVERSION}.0"
VIFileVersion    "${INFO_PRODUCTVERSION}.0"

VIAddVersionKey "CompanyName"     "${INFO_COMPANYNAME}"
VIAddVersionKey "FileDescription" "${INFO_PRODUCTNAME} Installer"
VIAddVersionKey "ProductVersion"  "${INFO_PRODUCTVERSION}"
VIAddVersionKey "FileVersion"     "${INFO_PRODUCTVERSION}"
VIAddVersionKey "LegalCopyright"  "${INFO_COPYRIGHT}"
VIAddVersionKey "ProductName"     "${INFO_PRODUCTNAME}"

ManifestDPIAware true

!include "MUI.nsh"

!define MUI_ICON "..\icon.ico"
!define MUI_UNICON "..\icon.ico"
!define MUI_FINISHPAGE_NOAUTOCLOSE
!define MUI_ABORTWARNING

!insertmacro MUI_PAGE_WELCOME
!insertmacro MUI_PAGE_DIRECTORY
!insertmacro MUI_PAGE_INSTFILES
!insertmacro MUI_PAGE_FINISH

# 卸载时提供一个默认不勾选的「同时删除连接配置与设置」选项。
!define MUI_COMPONENTSPAGE_NODESC
!insertmacro MUI_UNPAGE_COMPONENTS
!insertmacro MUI_UNPAGE_INSTFILES

!insertmacro MUI_LANGUAGE "English"

Name "${INFO_PRODUCTNAME}"
OutFile "..\..\bin\${INFO_PROJECTNAME}-${ARCH}-installer.exe"
InstallDir "$LOCALAPPDATA\CZL\${INFO_PRODUCTNAME}"
ShowInstDetails show

Function .onInit
   !insertmacro wails.checkArchitecture
FunctionEnd

!macro czl.stopRunning
    nsExec::Exec 'taskkill /F /IM "${PRODUCT_EXECUTABLE}"'
    Sleep 500
!macroend

Section
    !insertmacro wails.setShellContext
    !insertmacro wails.webview2runtime
    !insertmacro czl.stopRunning

    SetOutPath $INSTDIR
    !insertmacro wails.files

    CreateShortcut "$SMPROGRAMS\${INFO_PRODUCTNAME}.lnk" "$INSTDIR\${PRODUCT_EXECUTABLE}"
    CreateShortCut "$DESKTOP\${INFO_PRODUCTNAME}.lnk" "$INSTDIR\${PRODUCT_EXECUTABLE}"

    !insertmacro wails.writeUninstaller
SectionEnd

# 程序文件与用户数据同在安装目录下: 默认只删程序、缓存与日志, 保留 config\ 与 data\。
Section /o "un.同时删除连接配置与设置 (data、config)" SecPurge
    RMDir /r "$INSTDIR\config"
    RMDir /r "$INSTDIR\data"
SectionEnd

Section "-un.main"
    !insertmacro wails.setShellContext
    !insertmacro czl.stopRunning

    Delete "$INSTDIR\${PRODUCT_EXECUTABLE}"
    RMDir /r "$INSTDIR\cache" # 含 WebView2 数据目录
    RMDir /r "$INSTDIR\logs"

    Delete "$SMPROGRAMS\${INFO_PRODUCTNAME}.lnk"
    Delete "$DESKTOP\${INFO_PRODUCTNAME}.lnk"

    !insertmacro wails.deleteUninstaller
    # 只在已空时删除目录本身, 保留的用户数据不受影响。
    RMDir "$INSTDIR"
SectionEnd
