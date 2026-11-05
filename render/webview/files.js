(() => {
  'use strict';
  if (window.__gofastr_files) return;
  const maximum = 1048576;
  let current;
  let sequence = 0;

  const definitions = () => window.__gofastr_file_extension_definitions || [];
  const extensionCapabilities = () => Object.assign({}, ...definitions().map(adapter => adapter.capabilities()));
  const prepare = (files) => {
    const operation = files?.extension || (files?.photo ? { name: 'gofastr.photos/v1', payload: JSON.stringify(files.photo) } : null);
    if (!operation || typeof operation.name !== 'string' || typeof operation.payload !== 'string' ||
        operation.payload.length > 8192 || typeof TextEncoder !== 'function' || new TextEncoder().encode(operation.payload).length > 8192) return null;
    const adapter = definitions().find(candidate => candidate.name === operation.name);
    if (!adapter) return null;
    try {
      const prepared = adapter.prepare(JSON.parse(operation.payload));
      if (!prepared || !/^[a-z][a-z0-9-]{0,31}$/.test(prepared.kind) ||
          !['title', 'hint', 'label', 'progress'].every(key => typeof prepared[key] === 'string' && prepared[key].length <= 2048) ||
          !['supported', 'job', 'normalize'].every(key => typeof prepared[key] === 'function')) return null;
      return prepared;
    } catch (_) { return null; }
  };

  const element = (tag, text, attributes = {}) => {
    const node = document.createElement(tag);
    if (text) node.textContent = text;
    for (const [name, value] of Object.entries(attributes)) node.setAttribute(name, value);
    return node;
  };
  const controls = (view) => Array.from(view.dialog.querySelectorAll('button, input')).filter((control) => !control.disabled && !control.hidden);
  const focus = (view) => (controls(view)[0] || view.heading).focus({ preventScroll: true });
  const revoke = (view) => {
    if (!view.url) return;
    URL.revokeObjectURL(view.url);
    view.url = null;
  };
  const deferRevoke = (view) => {
    const url = view.url;
    setTimeout(() => { if (view.url === url) revoke(view); }, 0);
  };
  const close = (root, restore = true) => {
    if (!current || current.root !== root) return;
    const view = current;
    current = null;
    window.__gofastr_foreground?.release(view);
    view.photoJob?.cancel();
    view.photoJob = null;
    if (view.downloading) deferRevoke(view);
    else revoke(view);
    if (view.dialog.open && typeof view.dialog.close === 'function') view.dialog.close();
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
  const plainName = (name) => typeof name === 'string' && name.length > 0 && name !== '.' && name !== '..' &&
    typeof TextEncoder === 'function' && new TextEncoder().encode(name).length <= 200 && !/[/\\:\u0000-\u001f\u007f-\u009f]/.test(name);
  const mediaType = (value) => typeof value === 'string' && value.length <= 128 &&
    /^[a-z0-9!#$&^_.+-]+\/[a-z0-9!#$&^_.+-]+(?:\s*;[^\r\n\u0000-\u001f]*)?$/i.test(value);
  const decode = (data) => {
    if (typeof data !== 'string' || data.length > 4 * Math.ceil(maximum / 3) ||
        data.length % 4 !== 0 || !/^[A-Za-z0-9+/]*={0,2}$/.test(data)) throw new Error('Invalid file data.');
    const binary = atob(data);
    if (binary.length > maximum || btoa(binary) !== data) throw new Error('Invalid or oversized file data.');
    return Uint8Array.from(binary, (character) => character.charCodeAt(0));
  };
  const outcome = (error) => ({ status: error?.name === 'AbortError' ? 'cancelled' :
    ['NotAllowedError', 'SecurityError'].includes(error?.name) ? 'denied' : error?.name === 'NotSupportedError' ? 'unsupported' : 'failed' });
  const feedback = (view, message, error = false) => {
    view.feedback.textContent = message;
    view.feedback.setAttribute('aria-live', error ? 'assertive' : 'polite');
  };
  const lock = (view, busy) => {
    view.busy = busy;
    if (busy) view.dialog.setAttribute('aria-busy', 'true');
    else view.dialog.removeAttribute('aria-busy');
    for (const control of view.dialog.querySelectorAll('button, input')) control.disabled =
      (busy && !(view.photoJob && control === view.cancel)) || (view.sent && control !== view.recover);
  };
  const submit = async (view, result) => {
    if (current !== view || view.busy || view.sent || !view.root.isConnected) return;
    view.sent = true;
    lock(view, true);
    try {
      await view.respond(result, view.opener);
      if (current === view) close(view.root);
    } catch (error) {
      if (current !== view) return;
      view.sent = !error.fileRetryable;
      view.resume = error.complete;
      view.recover.hidden = !view.resume;
      feedback(view, error.message || 'The file result is uncertain. It was not retried.', true);
    } finally {
      if (current === view) {
        lock(view, false);
        focus(view);
      }
    }
  };
  const recover = async (view) => {
    if (current !== view || view.busy || !view.resume || !view.root.isConnected) return;
    lock(view, true);
    try {
      await view.resume();
      if (current === view) close(view.root);
    } catch (error) {
      if (current === view) feedback(view, error.message || 'The result is still uncertain. No file result was retried.', true);
    } finally {
      if (current === view) { lock(view, false); focus(view); }
    }
  };
  const cancel = async (view) => {
    if (view.photoJob) {
      view.photoJob.cancel();
      view.photoJob = null;
      lock(view, false);
    }
    await submit(view, { status: 'cancelled' });
  };
  const photo = async (view) => {
    if (current !== view || view.busy || view.sent) return;
    const selected = view.input.files;
    if (!selected?.length) { await cancel(view); return; }
    if (selected.length !== 1) { await submit(view, { status: 'denied' }); return; }
    const adapter = view.extension;
    if (!adapter?.supported()) { await submit(view, { status: 'unsupported' }); return; }
    let operation;
    try {
      operation = adapter.job();
      if (typeof operation?.cancel !== 'function') throw new Error('Invalid acquisition job.');
    } catch (error) { await submit(view, outcome(error)); return; }
    view.photoJob = operation;
    lock(view, true);
    feedback(view, adapter.progress);
    let result;
    try { result = { status: 'completed', file: await adapter.normalize(selected[0], operation) }; }
    catch (error) { result = outcome(error); }
    if (current !== view || view.photoJob !== operation || !view.root.isConnected) return;
    view.photoJob = null;
    lock(view, false);
    await submit(view, result);
  };
  const pick = async (view) => {
    if (current !== view || view.busy || view.sent) return;
    const selected = view.input.files;
    if (!selected?.length) { await submit(view, { status: 'cancelled' }); return; }
    const file = selected[0];
    const policy = view.files.pick;
    if (selected.length !== 1 || !plainName(file.name) || !Number.isSafeInteger(file.size) || file.size < 0 || file.size > policy.maxBytes ||
        (policy.extensions?.length && !policy.extensions.some((extension) => file.name.toLowerCase().endsWith(extension.toLowerCase())))) {
      feedback(view, 'That file does not meet the filename, size, or extension limits.', true);
      await submit(view, { status: 'denied' });
      return;
    }
    lock(view, true);
    feedback(view, 'Reading the selected file…');
    let result;
    try {
      if (typeof file.slice !== 'function' || typeof Blob.prototype.arrayBuffer !== 'function') result = { status: 'unsupported' };
      else {
        const bytes = new Uint8Array(await file.slice(0, policy.maxBytes + 1).arrayBuffer());
        if (bytes.length > policy.maxBytes || bytes.length !== file.size) result = { status: 'denied' };
        else {
          let binary = '';
          for (let offset = 0; offset < bytes.length; offset += 32768) binary += String.fromCharCode(...bytes.subarray(offset, offset + 32768));
          result = { status: 'completed', file: { name: file.name, mediaType: mediaType(file.type) ? file.type : 'application/octet-stream', data: btoa(binary) } };
        }
      }
    } catch (error) { result = outcome(error); }
    if (current !== view || !view.root.isConnected) return;
    lock(view, false);
    await submit(view, result);
  };
  const download = (view) => {
    if (current !== view || view.busy || view.sent) return;
    let result;
    try {
      const file = view.files.export;
      const link = element('a', '', { download: file.name, 'data-gfw-file-download-link': '', hidden: '' });
      if (typeof Blob !== 'function' || typeof URL.createObjectURL !== 'function' || typeof URL.revokeObjectURL !== 'function' || !('download' in link)) {
        result = { status: 'unsupported' };
      } else {
        const bytes = decode(file.data);
        revoke(view);
        view.url = URL.createObjectURL(new Blob([bytes], { type: file.mediaType }));
        link.setAttribute('href', view.url);
        view.dialog.append(link);
        view.downloading = true;
        try { link.click(); } finally { link.remove(); }
        feedback(view, 'Download initiated. This WebView cannot confirm an external disk save.');
        deferRevoke(view);
        result = { status: 'completed' };
      }
    } catch (error) { revoke(view); result = outcome(error); }
    void submit(view, result);
  };
  const valid = (files) => {
    if (!files || typeof files.id !== 'string' || !files.id) return false;
    const operations = [files.pick, files.export, files.photo, files.extension].filter((operation) => operation !== undefined && operation !== null);
    if (operations.length !== 1 || typeof operations[0] !== 'object' || Array.isArray(operations[0])) return false;
    if (files.photo || files.extension) return !!prepare(files);
    if (files.pick) return typeof files.pick.title === 'string' && !!files.pick.title && Number.isSafeInteger(files.pick.maxBytes) &&
      files.pick.maxBytes > 0 && files.pick.maxBytes <= maximum && (files.pick.extensions === undefined ||
        (Array.isArray(files.pick.extensions) && files.pick.extensions.length <= 16 && files.pick.extensions.every((extension) =>
          typeof extension === 'string' && /^\.[^./\\,;\s\u0000-\u001f\u007f-\u009f]{1,15}$/.test(extension))));
    return plainName(files.export.name) && mediaType(files.export.mediaType) && typeof files.export.data === 'string' &&
      files.export.data.length <= 4 * Math.ceil(maximum / 3);
  };
  const open = ({ root, files, fileId, opener = document.activeElement, respond }) => {
    if (!root.isConnected) return;
    if (current || window.__gofastr_presentations?.has() || window.__gofastr_services?.has()) throw new Error('Finish the open modal task first.');
    if (typeof fileId !== 'string' || !fileId) throw new Error('The file task has no issued identity.');
    const adapter = prepare(files);
    const prefix = 'gfw-file-' + ++sequence;
    const dialog = element('dialog', '', { 'data-gfw-file': adapter?.kind || (files?.pick ? 'pick' : 'export'), 'data-gfw-file-id': fileId,
      role: 'dialog', 'aria-modal': 'true', 'aria-labelledby': prefix + '-title', 'aria-describedby': prefix + '-hint' });
    if (adapter?.source) dialog.setAttribute('data-gfw-photo-source', adapter.source);
    const heading = element('h2', adapter?.title || files?.pick?.title || 'Export file', { id: prefix + '-title', tabindex: '-1' });
    const hint = element('p', adapter?.hint || (files?.pick ? 'Choose one file. Only the selected file is read, up to the requested limit.' :
      'Download initiates a browser download; this WebView cannot confirm an external disk save.'), { id: prefix + '-hint', class: 'gfw-file-hint' });
    const view = { root, dialog, heading, opener, files, extension: adapter, respond, busy: false, sent: false, url: null };
    const validRequest = valid(files);
    const supported = validRequest && typeof Blob === 'function' && typeof TextEncoder === 'function' &&
      typeof btoa === 'function' && typeof atob === 'function' && (adapter ? adapter.supported() &&
        element('input', '', { type: 'file' }).type === 'file' : files.pick ?
        typeof Blob.prototype.arrayBuffer === 'function' && element('input', '', { type: 'file' }).type === 'file' :
        typeof URL.createObjectURL === 'function' && typeof URL.revokeObjectURL === 'function' && 'download' in element('a'));
    dialog.append(heading, hint);
    const actions = element('div', '', { class: 'gfw-file-actions' });
    if (supported && (files.pick || adapter)) {
      const label = element('label', adapter?.label || 'Choose file', { for: prefix + '-input' });
      view.input = element('input', '', { type: 'file', id: prefix + '-input', 'data-gfw-file-input': '', 'aria-describedby': hint.id });
      if (adapter) {
        if (adapter.source) view.input.setAttribute('data-gfw-photo-source', adapter.source);
        for (const [name, value] of Object.entries(adapter.attributes || {})) {
          if (['accept', 'capture'].includes(name)) view.input.setAttribute(name, value);
        }
      } else if (files.pick.extensions?.length) view.input.setAttribute('accept', files.pick.extensions.join(','));
      view.input.addEventListener('change', () => void (adapter ? photo(view) : pick(view)));
      view.input.addEventListener('cancel', () => void cancel(view));
      dialog.append(label, view.input);
    } else {
      const button = element('button', supported ? 'Download' : 'Continue', { type: 'button', 'data-gfw-file-download': '' });
      button.addEventListener('click', () => supported ? download(view) : void submit(view, { status: validRequest ? 'unsupported' : 'failed' }));
      actions.append(button);
    }
    view.cancel = element('button', 'Cancel', { type: 'button', 'data-gfw-file-cancel': '' });
    view.cancel.addEventListener('click', () => void cancel(view));
    view.recover = element('button', 'Check result', { type: 'button', 'data-gfw-file-complete': '' });
    view.recover.hidden = true;
    view.recover.addEventListener('click', () => void recover(view));
    actions.append(view.cancel, view.recover);
    view.feedback = element('p', supported ? '' : validRequest ? 'File access is unavailable in this WebView.' : 'Invalid file request. No file was read or downloaded.', {
      'data-gfw-file-status': '', role: 'status', 'aria-live': 'polite', 'aria-atomic': 'true' });
    dialog.append(actions, view.feedback);
    dialog.addEventListener('cancel', (event) => { event.preventDefault(); void cancel(view); });
    dialog.addEventListener('keydown', (event) => {
      if (event.key === 'Escape') {
        event.preventDefault();
        event.stopPropagation();
        void cancel(view);
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
      if (typeof dialog.showModal === 'function') dialog.showModal();
      else dialog.setAttribute('open', '');
      focus(view);
    } catch (error) { close(root); throw error; }
  };
  document.addEventListener('focusin', (event) => {
    if (current && !current.dialog.contains(event.target)) focus(current);
  });
  window.addEventListener('beforeunload', (event) => {
    if (!current) return;
    event.preventDefault();
    event.returnValue = '';
  });
  window.navigation?.addEventListener('navigate', (event) => {
    if (current && event.cancelable && !(current.downloading && current.url && event.destination?.url === current.url)) event.preventDefault();
  });
  window.__gofastr_files = { open, close, has: (root) => !!current && (!root || current.root === root), capability: 'fallback',
    capabilities: extensionCapabilities, photoCapability: () => extensionCapabilities()['photos/v1'] || 'unavailable' };
  if (window.__gofastr) (window.__gofastr.loadedModules ||= {})['gfm-webview-files'] = true;
})();
