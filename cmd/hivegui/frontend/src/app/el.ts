// getElementById wrappers for the ids index.html owns. Neither treats
// the null half of the return as a runtime condition — a missing id is
// document/module drift, a load-time bug. They differ only in WHEN that
// bug surfaces, and the choice is forced by who imports the module.
//
// This file must stay side-effect free: the modal modules import it and
// are themselves pulled in transitively by view.ts and keyboard.ts.

// Throws, naming the id — preferred, and what dom.ts's app singletons
// use. Only safe in a module every importer is guaranteed to load with
// the markup present.
export function mustEl(id: string): HTMLElement {
  const el = document.getElementById(id);
  if (!el) throw new Error(`missing #${id} in index.html`);
  return el;
}

// Casts instead of throwing, preserving today's behavior exactly: a
// missing id surfaces as a TypeError at first use, not at import. Used
// by the modals and by banners.ts, which view.ts / keyboard.ts drag into
// jsdom tests that mount only the markup they exercise — those tests
// never open a modal or a banner, and a load-time throw would break them
// for elements they legitimately don't have.
//
// The type parameter names the element kind index.html declares (an
// <input>, a <select>) so call sites reach .value without a second cast.
export function pageEl<T extends HTMLElement = HTMLElement>(id: string): T {
  return document.getElementById(id) as T;
}
