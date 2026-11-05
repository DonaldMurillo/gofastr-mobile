(() => {
  'use strict';
  if (window.__gofastr_presentations) return;
  let current;
  let sequence = 0;

  const element = (tag, text, attributes = {}) => {
    const node = document.createElement(tag);
    if (text) node.textContent = text;
    for (const [name, value] of Object.entries(attributes)) node.setAttribute(name, value);
    return node;
  };

  const controls = (view) => Array.from(view.dialog.querySelectorAll('button, input')).filter((control) => !control.disabled);
  const focus = (view) => {
    const available = controls(view);
    const target = available.find((control) => control.checked) || available.find((control) => control.tagName === 'INPUT') ||
      available.find((control) => !control.dataset.gfwRole) || view.heading;
    target.focus({ preventScroll: true });
  };

  const close = (root, restore = true) => {
    if (!current || current.root !== root) return;
    const view = current;
    current = null;
    window.__gofastr_foreground?.release(view);
    if (view.dialog.open) view.dialog.close();
    view.dialog.remove();
    if (!restore) return;
    if (view.opener?.isConnected && !view.opener.disabled) view.opener.focus({ preventScroll: true });
    else if (root.isConnected) {
      const previous = root.getAttribute('tabindex');
      root.setAttribute('tabindex', '-1');
      root.focus({ preventScroll: true });
      if (previous === null) root.removeAttribute('tabindex');
      else root.setAttribute('tabindex', previous);
    }
  };

  const submit = async (view, choice) => {
    if (current !== view || view.busy || !view.root.isConnected) return;
    const selected = Array.from(view.dialog.querySelectorAll('[data-gfw-option]')).filter((option) => option.checked).map((option) => option.value);
    const disabled = new Map(Array.from(view.dialog.querySelectorAll('button, input')).map((control) => [control, control.disabled]));
    view.busy = true;
    view.dialog.setAttribute('aria-busy', 'true');
    view.feedback.textContent = 'Working…';
    for (const control of disabled.keys()) control.disabled = true;
    try {
      await view.respond(choice, selected, view.opener);
    } catch (error) {
      if (current === view) {
        view.feedback.textContent = error.message || 'Could not complete this response. Your selection is still here.';
        view.feedback.setAttribute('aria-live', 'assertive');
      }
    } finally {
      if (current === view) {
        view.busy = false;
        view.dialog.removeAttribute('aria-busy');
        for (const [control, wasDisabled] of disabled) control.disabled = wasDisabled;
        if (!view.dialog.contains(document.activeElement) || document.activeElement.disabled) focus(view);
      }
    }
  };

  const open = ({ root, presentation, presentationId, opener = document.activeElement, respond }) => {
    if (!root.isConnected) return;
    if (window.__gofastr_files?.has()) throw new Error('Finish the open file task first.');
    if (window.__gofastr_services?.has()) throw new Error('Finish the open foreground task first.');
    if (current) close(current.root, false);
    const prefix = 'gfw-presentation-' + ++sequence;
    const dialog = element('dialog', '', { 'data-gfw-presentation': presentation.kind, 'data-gfw-presentation-id': presentationId,
      'aria-modal': 'true', 'aria-labelledby': prefix + '-title', role: ['alert', 'confirmation'].includes(presentation.kind) ? 'alertdialog' : 'dialog' });
    const heading = element('h2', presentation.title || 'Choose an action', { id: prefix + '-title', tabindex: '-1' });
    dialog.append(heading);
    if (presentation.message) {
      const message = element('p', presentation.message, { id: prefix + '-message', class: 'gfw-presentation-message' });
      dialog.setAttribute('aria-describedby', message.id);
      dialog.append(message);
    }
    const view = { root, dialog, heading, opener, respond, busy: false, feedback: null, backdrop: false };
    if (presentation.selection) {
      const selection = presentation.selection;
      const fieldset = element('fieldset', '', { class: 'gfw-presentation-options' });
      fieldset.append(element('legend', 'Options'));
      const instruction = element('p', selection.multiple ? 'Select options.' : 'Select one option.', { id: prefix + '-selection', class: 'gfw-presentation-hint' });
      fieldset.setAttribute('aria-describedby', instruction.id);
      fieldset.append(instruction);
      for (const [index, option] of selection.options.entries()) {
        const label = element('label', '', { class: 'gfw-presentation-option' });
        const input = element('input', '', { type: selection.multiple ? 'checkbox' : 'radio', name: prefix + '-option',
          value: option.id, 'data-gfw-option': option.id, 'aria-labelledby': prefix + '-option-' + index });
        input.checked = (selection.selected || []).includes(option.id);
        input.disabled = option.disabled;
        const content = element('span');
        content.append(element('span', option.label, { id: prefix + '-option-' + index }));
        if (option.detail) {
          const detail = element('span', option.detail, { id: prefix + '-detail-' + index, class: 'gfw-presentation-detail' });
          input.setAttribute('aria-describedby', detail.id);
          content.append(detail);
        }
        label.append(input, content);
        fieldset.append(label);
      }
      dialog.append(fieldset);
    }
    const choices = element('div', '', { class: 'gfw-presentation-choices' });
    for (const choice of presentation.choices) {
      const button = element('button', choice.label, { type: 'button', 'data-gfw-choice': choice.id, 'data-gfw-role': choice.role || '' });
      button.disabled = choice.disabled;
      button.addEventListener('click', () => void submit(view, choice.id));
      choices.append(button);
    }
    dialog.append(choices);
    view.feedback = element('p', '', { class: 'gfw-presentation-status', 'data-gfw-presentation-status': '', role: 'status', 'aria-live': 'polite', 'aria-atomic': 'true' });
    dialog.append(view.feedback);
    const cancel = () => { if (presentation.dismissible) void submit(view, ''); };
    const outside = (event) => {
      const rectangle = dialog.getBoundingClientRect();
      return event.target === dialog && (event.clientX < rectangle.left || event.clientX > rectangle.right || event.clientY < rectangle.top || event.clientY > rectangle.bottom);
    };
    dialog.addEventListener('cancel', (event) => { event.preventDefault(); cancel(); });
    dialog.addEventListener('pointerdown', (event) => { view.backdrop = outside(event); });
    dialog.addEventListener('click', (event) => { if (view.backdrop && outside(event)) cancel(); view.backdrop = false; });
    dialog.addEventListener('keydown', (event) => {
      if (event.key === 'Escape') {
        event.preventDefault();
        event.stopPropagation();
        cancel();
      } else if (event.key === 'Tab') {
        const available = controls(view);
        const first = available[0] || heading;
        const last = available[available.length - 1] || heading;
        if (!available.length || !available.includes(document.activeElement) || (event.shiftKey ? document.activeElement === first : document.activeElement === last)) {
          event.preventDefault();
          (event.shiftKey ? last : first).focus({ preventScroll: true });
        }
      }
    });
    window.__gofastr_foreground?.claim(view);
    current = view;
    (root.querySelector('[data-gfm-platform]') || root).append(dialog);
    try {
      dialog.showModal();
      focus(view);
    } catch (error) {
      close(root);
      throw error;
    }
  };

  document.addEventListener('focusin', (event) => {
    if (current && !current.dialog.contains(event.target)) focus(current);
  });
  window.__gofastr_presentations = { open, close, has: (root) => !!current && (!root || current.root === root) };
  if (window.__gofastr) (window.__gofastr.loadedModules ||= {})['gfm-webview-presentation'] = true;
})();
