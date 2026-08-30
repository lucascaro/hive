# Fix raw escape characters in the update banner

Build output shown in the update banner is now plain text — ANSI colour codes,
cursor-control sequences and carriage-return redraws from `build.sh` no longer
leak through as literal `ESC[32m` garbage.
