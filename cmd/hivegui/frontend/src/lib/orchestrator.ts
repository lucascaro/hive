// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

// Shown wherever the user grants "May direct other sessions" (spec 389),
// so the grant is not read as a security boundary. SECURITY.md › Trust
// model has the long form.
export const ORCHESTRATOR_GRANT_HINT =
  "Limits only agents that use Hive's messaging. Any agent with a shell can reach Hive directly.";
