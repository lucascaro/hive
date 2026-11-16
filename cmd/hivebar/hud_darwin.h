// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

// The AppKit and Carbon half of the dictation HUD (spec 553). Every
// function returns at once: the work is queued onto the main thread,
// and strings are copied before the call returns.
#include <stdint.h>

void hbHotkeySet(uint32_t keyCode, uint32_t modifiers, int enabled);
// labels holds n target labels separated by '\x1f'.
void hbHUDShow(const char *labels, int n, int selected, const char *status,
               const char *hints, int clear);
void hbHUDHide(int restore);
