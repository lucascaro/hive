// @vitest-environment jsdom
import { describe, expect, it, vi } from 'vitest';
import {
  colorInput,
  errorSlot,
  field,
  selectInput,
  textInput,
  textareaInput,
} from '../../src/ui/field';

describe('field primitives', () => {
  it('associates the label with the control it wraps', () => {
    const input = textInput({ value: 'x' });
    const l = field('Name', input);
    document.body.replaceChildren(l);
    expect(l.querySelector('.hv-field__label')?.textContent).toBe('Name');
    expect(l.contains(input)).toBe(true);
    // A wrapping <label> associates implicitly; no id/for bookkeeping.
    expect(input.closest('label')).toBe(l);
  });

  it('reports input through the callback and keeps the aria-label', () => {
    const onInput = vi.fn();
    const i = textInput({ ariaLabel: 'Agent name', onInput });
    i.value = 'typed';
    i.dispatchEvent(new Event('input'));
    expect(onInput).toHaveBeenCalledWith('typed');
    expect(i.getAttribute('aria-label')).toBe('Agent name');
  });

  it('builds a select from options and reports change', () => {
    const onChange = vi.fn();
    const s = selectInput({
      options: [
        { value: 'a', label: 'A' },
        { value: 'b', label: 'B' },
      ],
      value: 'b',
      onChange,
    });
    expect([...s.options].map((o) => o.value)).toEqual(['a', 'b']);
    expect(s.value).toBe('b');
    s.value = 'a';
    s.dispatchEvent(new Event('change'));
    expect(onChange).toHaveBeenCalledWith('a');
  });

  it('wraps the native colour picker in a swatch that mirrors the value', () => {
    const { el, input } = colorInput({ value: '#112233', ariaLabel: 'Colour' });
    expect(input.type).toBe('color');
    expect(el.style.getPropertyValue('--swatch')).toBe('#112233');
    input.value = '#445566';
    input.dispatchEvent(new Event('input'));
    expect(el.style.getPropertyValue('--swatch')).toBe('#445566');
  });

  it('error slot is hidden when empty and announces when not', () => {
    const e = errorSlot();
    expect(e.el.getAttribute('role')).toBe('alert');
    expect(e.el.classList.contains('hidden')).toBe(true);
    e.show('it broke');
    expect(e.el.textContent).toBe('it broke');
    expect(e.el.classList.contains('hidden')).toBe(false);
    e.clear();
    expect(e.el.classList.contains('hidden')).toBe(true);
  });

  it('textarea passes rows and id through', () => {
    const t = textareaInput({ rows: 4, id: 'ov', value: '--accent: red;' });
    expect(t.rows).toBe(4);
    expect(t.id).toBe('ov');
    expect(t.value).toBe('--accent: red;');
  });
});
