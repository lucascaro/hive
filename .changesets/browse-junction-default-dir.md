---
type: fixed
bump: patch
---
- **New Project's Browse… button works again for a Hive launched from a junction on Windows.** Wails checks the picker's starting folder with `Lstat`, so a junction such as `C:\Users\me\git` → `D:\git` was refused as "does not exist" and the folder dialog never opened — silently, because the project editor dropped the error. The starting folder is now resolved to its real path first, the dialog is retried with no starting folder if the runtime still refuses it, and a picker that fails to open is reported in the status bar. Separately, a Hive started from the Start menu or a shortcut on Windows now writes `hivegui.log`: the logger stopped at the first failed write to the (absent) console before reaching the file.
