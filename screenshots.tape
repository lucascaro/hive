# Hive TUI Screenshots
# Run with: ~/go/bin/vhs screenshots.tape

Output screenshots/sidebar.png
Output screenshots/sidebar.gif

Set Shell fish
Set FontSize 14
Set Width 1400
Set Height 800
Set Theme "Catppuccin Mocha"
Set Padding 20
Set TypingSpeed 0.2
Set Framerate 30

Type "hive start"
Enter
Sleep 3s
Screenshot screenshots/1-main-view.png
Sleep 2s

# Grid View
Type "g"
Sleep 2s
Screenshot screenshots/2-grid-view.png

# Multi Grid View
Type "G"
Sleep 2s
Screenshot screenshots/3-all-grid-view.png
Sleep 2s

# # Add a project
# Type "n"
# Sleep 1s
# Screenshot screenshots/add-project-1a.png
# # Project Name
# Type "Test project 1"
# Enter
# Sleep 1s
# Screenshot screenshots/add-project-1b.png
# # Directory
# Type "."
# Sleep 1s
# Screenshot screenshots/add-project-1b.png

# # Add sessions
# Type "t"
# Screenshot screenshots/add-project-1-s1a.png
# Sleep 1s
# Type "claude"
# Enter
# Screenshot screenshots/add-project-1-s1b.png
# Sleep 2s
