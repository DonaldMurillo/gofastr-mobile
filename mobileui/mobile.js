(() => {
  'use strict';
  const runtime = window.__gofastr;
  if (document.__gfmMobileBound) return;
  document.__gfmMobileBound = true;
  const initialized = new WeakSet();
  const draftKey = (form) => 'gfm-note-draft:' + (form.dataset.gfmEditor || 'new');
  const status = (form, message, error = false) => {
    const live = form.querySelector('[data-gfm-status]');
    live.setAttribute('aria-live', error ? 'assertive' : 'polite');
    live.textContent = message;
    form.dataset.gfmError = String(error);
  };
  const remember = (form) => {
    try {
      sessionStorage.setItem(draftKey(form), JSON.stringify({
        title: form.elements.title.value, body: form.elements.body.value,
      }));
      status(form, 'Unsaved changes · draft kept for this session');
    } catch (_) {
      status(form, 'Unsaved changes · save before leaving this screen');
    }
  };
  const scan = () => {
    for (const form of document.querySelectorAll('[data-gfm-editor]')) {
      if (initialized.has(form)) continue;
      initialized.add(form);
      try {
        const raw = sessionStorage.getItem(draftKey(form));
        if (raw && form.dataset.gfmError !== 'true') {
          const draft = JSON.parse(raw);
          if (typeof draft.title === 'string' && typeof draft.body === 'string') {
            form.elements.title.value = draft.title;
            form.elements.body.value = draft.body;
            status(form, 'Unsaved draft restored · save when you’re ready');
          }
        }
      } catch (_) {}
      grow(form.elements.body);
    }
  };
  const grow = (field) => {
    field.style.height = 'auto';
    field.style.height = field.scrollHeight + 'px';
  };
  document.addEventListener('input', (event) => {
    const form = event.target.closest('[data-gfm-editor]');
    if (form) {
      remember(form);
      if (event.target === form.elements.body) grow(event.target);
    }
  });
  document.addEventListener('submit', async (event) => {
    const form = event.target;
    if (!(form instanceof HTMLFormElement)) return;
    if (form.matches('.gfm-search')) {
      if (typeof runtime?.navigate !== 'function') return;
      event.preventDefault();
      const destination = new URL(form.action, location.href);
      const search = form.elements.q.value.trim();
      if (search) destination.searchParams.set('q', search);
      runtime.navigate(destination.pathname + destination.search);
      return;
    }
    if (!form.matches('[data-gfm-editor]') || typeof runtime?.navigate !== 'function') return;
    event.preventDefault();
    if (form.dataset.gfmSaving === 'true') return;
    form.dataset.gfmSaving = 'true';
    const submitted = { title: form.elements.title.value, body: form.elements.body.value };
    const button = document.querySelector('button[form="' + form.id + '"]');
    if (button) button.disabled = true;
    form.setAttribute('aria-busy', 'true');
    status(form, 'Saving on this device…');
    try {
      const transport = window.__gofastr_transport || window.fetch.bind(window);
      const response = await transport(form.action, {
        method: 'POST', headers: { Accept: 'application/json' }, body: new FormData(form),
      });
      const result = await response.json();
      if (!response.ok) throw new Error(result.error || 'Could not save. Your text is still here.');
      runtime.invalidate('/notes', '/', result.destination.split('?')[0]);
      const previousDraftKey = draftKey(form);
      form.elements.id.value = result.id;
      form.dataset.gfmEditor = result.id;
      try { sessionStorage.removeItem(previousDraftKey); } catch (_) {}
      if (form.elements.title.value !== submitted.title || form.elements.body.value !== submitted.body) {
        remember(form);
        status(form, 'Earlier version saved · save again to keep your latest changes');
        return;
      }
      try { sessionStorage.removeItem(draftKey(form)); } catch (_) {}
      status(form, 'Saved on this device');
      runtime.navigate(result.destination, { replace: true, force: true });
    } catch (error) {
      status(form, error.message || 'Could not save. Your text is still here.', true);
    } finally {
      form.dataset.gfmSaving = 'false';
      form.removeAttribute('aria-busy');
      if (button) button.disabled = false;
    }
  }, true);
  new MutationObserver(scan).observe(document.documentElement, { childList: true, subtree: true });
  scan();
  (runtime.loadedModules ||= {})['gfm-mobile'] = true;
})();
