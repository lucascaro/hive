// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

const never = new Promise(() => {});
export default function activate() {
  return {
    settings: () => {
      throw never;
    },
  };
}
