(() => {
  'use strict';
  if (window.__gofastr_services) return;
  const maximum = 1048576;
  const statuses = ['completed', 'cancelled', 'denied', 'unsupported', 'failed'];
  const issued = new Set();
  const playback = new Map();
  const playbackSources = new Map();
  let current;
  let sequence = 0;
  let discovered;
  const element = (tag, text, attributes = {}) => {
    const node = document.createElement(tag);
    if (text) node.textContent = text;
    for (const [name, value] of Object.entries(attributes)) node.setAttribute(name, value);
    return node;
  };
  const bridge = () => {
    const injected = window.__gofastrDeviceServices;
    if (typeof injected?.capabilities === 'function' && typeof injected?.perform === 'function') return injected;
    const handler = window.webkit?.messageHandlers?.gofastrServices;
    if (typeof handler?.postMessage === 'function') return {
      capabilities: () => handler.postMessage({ kind: 'capabilities' }),
      perform: request => handler.postMessage({ kind: 'perform', request }),
    };
    return null;
  };
  const audioType = () => {
    if (!window.isSecureContext || typeof navigator.mediaDevices?.getUserMedia !== 'function' ||
        typeof window.MediaRecorder !== 'function' || typeof Blob !== 'function' ||
        typeof Blob.prototype.arrayBuffer !== 'function' || typeof btoa !== 'function' ||
        typeof URL.createObjectURL !== 'function' || typeof URL.revokeObjectURL !== 'function') return '';
    return ['audio/webm;codecs=opus', 'audio/mp4', 'audio/ogg;codecs=opus', 'audio/wav']
      .find(type => window.MediaRecorder.isTypeSupported?.(type)) || '';
  };
  const fallbackCapabilities = () => ({
    'notifications/v1': 'unavailable',
    'sharing/v1': typeof navigator.share === 'function' || typeof Blob === 'function' && typeof atob === 'function' && typeof URL.createObjectURL === 'function' ? 'fallback' : 'unavailable',
    'clipboard/v1': window.isSecureContext && typeof navigator.clipboard?.writeText === 'function' ? 'fallback' : 'unavailable',
    'audio/v1': audioType() ? 'fallback' : 'unavailable',
    'live/v1': 'available',
  });
  const capabilities = () => discovered ||= (async () => {
    const host = bridge();
    if (!host) return fallbackCapabilities();
    let facts;
    let timer;
    try { facts = await Promise.race([host.capabilities(), new Promise(resolve => { timer = setTimeout(resolve, 2000); })]); } catch (_) {}
    finally { clearTimeout(timer); }
    const result = { 'live/v1': 'available' };
    for (const capability of ['notifications/v1', 'sharing/v1', 'clipboard/v1', 'audio/v1']) {
      result[capability] = ['available', 'fallback', 'unavailable'].includes(facts?.[capability]) ? facts[capability] : 'unavailable';
    }
    return result;
  })();
  const kindOf = request => {
    const kinds = ['notification', 'share', 'clipboard', 'audio'].filter(key => request?.[key] != null);
    return kinds.length === 1 ? kinds[0] : '';
  };
  const bytesOf = file => {
    if (!file || typeof file.name !== 'string' || !file.name || file.name.length > 200 || /[/\\:\u0000-\u001f\u007f]/.test(file.name) ||
        typeof file.mediaType !== 'string' || !/^[a-z0-9!#$&^_.+-]+\/[a-z0-9!#$&^_.+-]+(?:;[^\r\n]*)?$/i.test(file.mediaType) ||
        typeof file.data !== 'string' || !file.data || file.data.length > 4 * Math.ceil(maximum / 3) ||
        !/^[A-Za-z0-9+/]+={0,2}$/.test(file.data) || file.data.length % 4 !== 0) throw new Error('Invalid or oversized private file.');
    const raw = atob(file.data);
    if (!raw.length || raw.length > maximum) throw new Error('Invalid or oversized private file.');
    return Uint8Array.from(raw, character => character.charCodeAt(0));
  };
  const audioBytes = file => {
    const bytes = bytesOf(file);
    const text = (offset, length) => String.fromCharCode(...bytes.slice(offset, offset + length));
    const type = file.mediaType.split(';')[0].toLowerCase();
    const valid = type === 'audio/webm' ? bytes.length >= 16 && bytes[0] === 26 && bytes[1] === 69 && bytes[2] === 223 && bytes[3] === 163 :
      type === 'audio/ogg' ? bytes.length >= 32 && text(0, 4) === 'OggS' :
      ['audio/mp4', 'audio/m4a', 'audio/x-m4a'].includes(type) ? bytes.length >= 16 && text(4, 4) === 'ftyp' :
      type === 'audio/aac' ? bytes.length >= 7 && bytes[0] === 255 && (bytes[1] & 246) === 240 :
      ['audio/wav', 'audio/wave', 'audio/x-wav'].includes(type) ? bytes.length >= 44 && text(0, 4) === 'RIFF' && text(8, 4) === 'WAVE' : false;
    if (!valid) throw new Error('The recording does not contain supported audio.');
    return bytes;
  };
  const feedback = (view, message, error = false) => {
    if (current !== view) return;
    view.feedback.textContent = message;
    view.feedback.setAttribute('aria-live', error ? 'assertive' : 'polite');
  };
  const stopTracks = view => {
    for (const track of view.stream?.getTracks() || []) { track.onended = null; track.onmute = null; track.stop(); }
    view.stream = null;
  };
  const cleanupAudio = view => {
    clearTimeout(view.timer);
    if (view.recorder) {
      view.recorder.ondataavailable = null;
      view.recorder.onstop = null;
      view.recorder.onerror = null;
      if (view.recorder.state !== 'inactive') { try { view.recorder.stop(); } catch (_) {} }
      view.recorder = null;
    }
    stopTracks(view);
    if (view.preview) { view.preview.pause?.(); view.preview.removeAttribute('src'); view.preview.load?.(); }
    if (view.url) URL.revokeObjectURL(view.url);
    view.url = null;
    view.chunks = [];
    view.blob = null;
  };
  const close = (root, restore = true) => {
    if (!current || current.root !== root) return;
    const view = current;
    current = null;
    cleanupAudio(view);
    for (const url of view.downloadURLs) URL.revokeObjectURL(url);
    view.downloadURLs.clear();
    window.__gofastr_foreground?.release(view);
    if (view.dialog.open) view.dialog.close?.();
    view.dialog.remove();
    if (restore && view.opener?.isConnected && !view.opener.disabled) view.opener.focus({ preventScroll: true });
  };
  const lock = (view, busy) => {
    view.busy = busy;
    view.dialog.setAttribute('aria-busy', String(busy));
    for (const button of view.dialog.querySelectorAll('button')) button.disabled = busy || view.sent && !button.hasAttribute('data-gfw-service-complete');
  };
  const settle = async (view, outcome) => {
    if (current !== view || view.sent || !view.root.isConnected) return;
    view.sent = true;
    cleanupAudio(view);
    lock(view, true);
    try { await view.respond(outcome, view.opener); if (current === view) close(view.root); }
    catch (error) {
      if (current !== view) return;
      view.resume = error.complete;
      view.completion.hidden = !view.resume;
      feedback(view, error.message || 'The result is uncertain. The device operation was not retried.', true);
    } finally { if (current === view) lock(view, false); }
  };
  const cancel = view => {
    if (current !== view || view.sent || view.hostPending || view.sharing) return;
    void settle(view, { status: 'cancelled', message: 'Service cancelled. Nothing was attached.' });
  };
  const hostResult = (view, result) => {
    if (!result || typeof result !== 'object' || !statuses.includes(result.status)) throw new Error('The device host returned an invalid result. It was not retried.');
    const outcome = { status: result.status };
    for (const key of ['message', 'permission', 'identifier']) if (typeof result[key] === 'string') outcome[key] = result[key];
    if (view.kind === 'audio' && result.status === 'completed') {
      if (!Number.isInteger(result.durationMillis) || result.durationMillis <= 0 || result.durationMillis > (view.request.audio.maxSeconds || 60) * 1000 ||
          audioBytes(result.file).length > (view.request.audio.maxBytes || maximum)) throw new Error('The device recording exceeded its admitted limits.');
      outcome.file = result.file;
      outcome.durationMillis = result.durationMillis;
    } else if (result.file) throw new Error('Only accepted audio can return private bytes.');
    return outcome;
  };
  const performHost = async view => {
    if (current !== view || view.started || view.sent) return;
    view.started = true;
    view.hostPending = true;
    lock(view, true);
    try { await settle(view, hostResult(view, await view.host.perform(view.request))); }
    catch (error) { await settle(view, { status: 'failed', message: 'The device outcome could not be confirmed. It was not retried. ' + (error.message || '') }); }
    finally { view.hostPending = false; if (current === view) lock(view, false); }
  };
  const button = (view, label, selector, handler) => {
    const control = element('button', label, { type: 'button', [selector]: '' });
    control.addEventListener('click', () => { if (!control.disabled && current === view && !view.sent) handler(); });
    view.actions.append(control);
    return control;
  };
  const shareFiles = request => {
    if (!Array.isArray(request.files || []) || (request.files || []).length > 4 ||
        new TextEncoder().encode(request.text || '').length > 65536) throw new Error('The share request exceeds its limits.');
    let total = 0;
    return (request.files || []).map(file => {
      const bytes = bytesOf(file);
      total += bytes.length;
      if (total > maximum) throw new Error('Shared files exceed 1 MiB.');
      return { file, bytes };
    });
  };
  const fallbackShare = view => {
    const request = view.request.share;
    const files = shareFiles(request);
    let data = { title: request.title || '', text: request.text || '' };
    if (files.length && typeof File === 'function') data.files = files.map(({ file, bytes }) => new File([bytes], file.name, { type: file.mediaType }));
    const canShare = typeof navigator.share === 'function' && (!files.length || data.files && typeof navigator.canShare === 'function' && navigator.canShare(data));
    if (canShare) button(view, 'Share…', 'data-gfw-service-perform', async () => {
      if (view.started) return;
      view.started = true;
      view.sharing = true;
      lock(view, true);
      try { await navigator.share(data); await settle(view, { status: 'completed', message: 'Shared using the system share sheet.' }); }
      catch (error) { await settle(view, { status: error.name === 'AbortError' ? 'cancelled' : 'failed', message: error.name === 'AbortError' ? 'Sharing cancelled.' : 'System sharing failed. It was not retried.' }); }
      finally { view.sharing = false; if (current === view) lock(view, false); }
    });
    else {
      feedback(view, 'System sharing is unavailable. Explicit downloads are not sharing, and an external disk save cannot be confirmed.');
      const downloads = [...files];
      if (request.text) downloads.unshift({ file: { name: 'shared-text.txt', mediaType: 'text/plain' }, bytes: new TextEncoder().encode(request.text) });
      const supported = typeof Blob === 'function' && typeof URL.createObjectURL === 'function' && typeof URL.revokeObjectURL === 'function';
      if (!supported) feedback(view, 'System sharing and explicit file downloads are unavailable in this browser.');
      for (const { file, bytes } of supported ? downloads : []) button(view, 'Download ' + file.name, 'data-gfw-service-download', () => {
        const url = URL.createObjectURL(new Blob([bytes], { type: file.mediaType }));
        view.downloadURLs.add(url);
        const link = element('a', '', { href: url, download: file.name, 'data-gfw-service-download-link': '' });
        link.addEventListener('click', event => event.stopPropagation());
        view.dialog.append(link);
        link.click();
        link.remove();
        setTimeout(() => { if (view.downloadURLs.delete(url)) URL.revokeObjectURL(url); }, 0);
        feedback(view, 'Download initiated. This is not sharing; an external disk save cannot be confirmed.');
      });
      button(view, supported ? 'Finish downloads' : 'Continue', 'data-gfw-service-perform', () => void settle(view, { status: 'unsupported', message: supported ? 'System sharing unavailable. Explicit browser downloads were offered, not actual sharing.' : 'System sharing and browser downloads are unavailable.' }));
    }
  };
  const record = async view => {
    if (view.started || current !== view) return;
    view.started = true;
    view.record.disabled = true;
    feedback(view, 'Requesting microphone access…');
    try {
      const stream = await navigator.mediaDevices.getUserMedia({ audio: true });
      if (current !== view || view.sent || !view.root.isConnected || document.hidden) { for (const track of stream.getTracks()) track.stop(); return; }
      view.stream = stream;
      for (const track of stream.getTracks()) { track.onended = () => cancel(view); track.onmute = () => cancel(view); }
      const recorder = new window.MediaRecorder(stream, { mimeType: audioType() });
      view.recorder = recorder;
      view.chunks = [];
      view.size = 0;
      view.startedAt = Date.now();
      view.pauseStarted = 0;
      view.pausedMillis = 0;
      recorder.ondataavailable = event => {
        if (current !== view || view.sent || !event.data?.size) return;
        view.size += event.data.size;
        if (view.size > view.maxBytes) { void settle(view, { status: 'failed', message: 'Recording exceeded its byte limit. No audio was attached.' }); return; }
        view.chunks.push(event.data);
      };
      recorder.onerror = () => void settle(view, { status: 'failed', message: 'Recording was interrupted. No audio was attached.' });
      recorder.onstop = () => {
        if (current !== view || view.sent) return;
        clearTimeout(view.timer);
        stopTracks(view);
        view.durationMillis = Math.min(view.maxSeconds * 1000, Math.max(1, (view.stoppedAt || Date.now()) - view.startedAt - view.pausedMillis - (view.pauseStarted ? (view.stoppedAt || Date.now()) - view.pauseStarted : 0)));
        view.blob = new Blob(view.chunks, { type: recorder.mimeType });
        view.chunks = [];
        if (!view.blob.size || view.blob.size > view.maxBytes) { void settle(view, { status: 'failed', message: 'The recording was empty or oversized. No audio was attached.' }); return; }
        view.url = URL.createObjectURL(view.blob);
        view.preview.src = view.url;
        view.preview.hidden = false;
        view.accept.hidden = false;
        view.pause.hidden = view.resumeButton.hidden = view.stop.hidden = true;
        feedback(view, 'Preview the recording, then accept it or cancel. It has not been attached.');
      };
      recorder.start(200);
      view.record.hidden = true;
      view.pause.hidden = view.stop.hidden = false;
      view.timer = setTimeout(() => stopRecording(view), view.maxSeconds * 1000);
      feedback(view, `Recording · maximum ${view.maxSeconds} seconds / ${view.maxBytes} bytes.`);
    } catch (error) { await settle(view, { status: ['NotAllowedError', 'SecurityError'].includes(error.name) ? 'denied' : 'failed', permission: 'microphone', message: 'Microphone access or recording is unavailable. No audio was attached.' }); }
  };
  const stopRecording = view => {
    if (current !== view || !view.recorder || view.recorder.state === 'inactive') return;
    view.stoppedAt = Date.now();
    view.stop.disabled = view.pause.disabled = view.resumeButton.disabled = true;
    view.recorder.stop();
    stopTracks(view);
    feedback(view, 'Preparing the recording preview…');
  };
  const fallbackAudio = view => {
    view.maxSeconds = view.request.audio.maxSeconds || 60;
    view.maxBytes = view.request.audio.maxBytes || maximum;
    if (!Number.isInteger(view.maxSeconds) || view.maxSeconds < 1 || view.maxSeconds > 60 || !Number.isInteger(view.maxBytes) || view.maxBytes < 1 || view.maxBytes > maximum) throw new Error('Invalid recording limits.');
    if (!audioType()) {
      feedback(view, 'Secure, supported MediaRecorder microphone recording is unavailable.');
      button(view, 'Continue', 'data-gfw-service-perform', () => void settle(view, { status: 'unsupported', message: 'Microphone recording is unavailable in this browser.' }));
      return;
    }
    feedback(view, 'Microphone permission is requested only when you press Record. Recording stops on interruption or departure.');
    view.preview = element('audio', '', { controls: '', preload: 'none', 'aria-label': 'Recording preview', 'data-gfw-audio-preview': '' });
    view.preview.hidden = true;
    view.dialog.append(view.preview);
    view.record = button(view, 'Record', 'data-gfw-audio-record', () => void record(view));
    view.pause = button(view, 'Pause', 'data-gfw-audio-pause', () => {
      if (view.recorder?.state !== 'recording') return;
      view.recorder.pause(); view.pauseStarted = Date.now();
      view.pause.hidden = true; view.resumeButton.hidden = false;
      feedback(view, 'Recording paused. The recording time limit remains active.');
    });
    view.resumeButton = button(view, 'Resume', 'data-gfw-audio-resume', () => {
      if (view.recorder?.state !== 'paused') return;
      view.recorder.resume(); view.pausedMillis += Date.now() - view.pauseStarted; view.pauseStarted = 0;
      view.pause.hidden = false; view.resumeButton.hidden = true;
      feedback(view, 'Recording resumed.');
    });
    view.stop = button(view, 'Stop', 'data-gfw-audio-stop', () => stopRecording(view));
    view.accept = button(view, 'Accept recording', 'data-gfw-audio-accept', async () => {
      if (view.accepting || !view.blob) return;
      view.accepting = true;
      lock(view, true);
      try {
        const bytes = new Uint8Array(await view.blob.arrayBuffer());
        if (current !== view || view.sent || !view.root.isConnected) return;
        let raw = '';
        for (const byte of bytes) raw += String.fromCharCode(byte);
        const mediaType = view.blob.type.split(';')[0];
        const extension = { 'audio/webm': 'webm', 'audio/mp4': 'm4a', 'audio/ogg': 'ogg', 'audio/wav': 'wav' }[mediaType];
        const file = { name: 'recording.' + (extension || 'audio'), mediaType, data: btoa(raw) };
        audioBytes(file);
        await settle(view, { status: 'completed', file, durationMillis: view.durationMillis });
      } catch (error) { await settle(view, { status: 'failed', message: error.message || 'The recording could not be accepted.' }); }
      finally { if (current === view) lock(view, false); }
    });
    view.pause.hidden = view.resumeButton.hidden = view.stop.hidden = view.accept.hidden = true;
  };
  const open = ({ root, request, serviceId, opener = document.activeElement, respond }) => {
    if (!root.isConnected) return;
    if (current || window.__gofastr_presentations?.has() || window.__gofastr_files?.has()) throw new Error('Finish the open foreground task first.');
    const kind = kindOf(request);
    if (!kind || typeof serviceId !== 'string' || !serviceId || issued.has(serviceId)) throw new Error('The service has no new issued identity. It was not repeated.');
    const prefix = 'gfw-service-' + ++sequence;
    const dialog = element('dialog', '', { 'data-gfw-service': kind, 'data-gfw-service-id': serviceId, role: 'dialog', 'aria-modal': 'true', 'aria-labelledby': prefix + '-title', 'aria-describedby': prefix + '-status' });
    const heading = element('h2', request[kind].title || { notification: 'Reminder', share: 'Share', clipboard: 'Copy text', audio: 'Record audio' }[kind], { id: prefix + '-title', tabindex: '-1' });
    const view = { root, request: JSON.parse(JSON.stringify(request)), kind, dialog, heading, opener, respond, host: bridge(), downloadURLs: new Set(), chunks: [] };
    window.__gofastr_foreground?.claim(view);
    issued.add(serviceId);
    current = view;
    view.feedback = element('p', '', { role: 'status', 'aria-live': 'polite', id: prefix + '-status', 'data-gfw-service-status': '' });
    view.actions = element('div', '', { class: 'gfw-service-actions' });
    dialog.append(heading, view.feedback, view.actions);
    try {
      if (view.host) button(view, { notification: request.notification?.operation === 'schedule' ? 'Schedule reminder' : request.notification?.operation === 'cancel' ? 'Cancel reminder' : 'Check reminder status', share: 'Share…', clipboard: 'Copy text', audio: 'Record audio…' }[kind], 'data-gfw-service-perform', () => void performHost(view));
      else if (kind === 'notification') {
        feedback(view, 'Scheduled reminders are unavailable in a plain browser. No timer or notification was created.');
        button(view, 'Continue', 'data-gfw-service-perform', () => void settle(view, { status: 'unsupported', message: 'Scheduled reminders require a native device host. No reminder was scheduled.' }));
      } else if (kind === 'clipboard') {
        if (new TextEncoder().encode(request.clipboard.text || '').length > 65536) throw new Error('Clipboard text exceeds 64 KiB.');
        const text = element('textarea', '', { readonly: '', 'aria-label': 'Text to copy', 'data-gfw-service-text': '' });
        text.value = request.clipboard.text || '';
        dialog.append(text);
        const supported = window.isSecureContext && typeof navigator.clipboard?.writeText === 'function';
        feedback(view, supported ? 'Press Copy text to write to the clipboard.' : 'Clipboard access is unavailable. Select and copy the text manually.');
        button(view, supported ? 'Copy text' : 'Continue', 'data-gfw-service-perform', async () => {
          if (view.started) return;
          view.started = true;
          lock(view, true);
          try {
            if (supported) await navigator.clipboard.writeText(request.clipboard.text || '');
            await settle(view, { status: supported ? 'completed' : 'unsupported', message: supported ? 'Text copied.' : 'Automatic clipboard access is unavailable. Manual copying was offered.' });
          } catch (_) { await settle(view, { status: 'denied', permission: 'clipboard', message: 'Clipboard access was denied. Nothing was copied automatically.' }); }
        });
      } else if (kind === 'share') fallbackShare(view);
      else fallbackAudio(view);
    } catch (error) {
      feedback(view, error.message || 'This service is unavailable.', true);
      button(view, 'Continue', 'data-gfw-service-perform', () => void settle(view, { status: 'failed', message: error.message || 'Invalid service request.' }));
    }
    view.cancelButton = button(view, 'Cancel', 'data-gfw-service-cancel', () => cancel(view));
    view.completion = element('button', 'Check completion', { type: 'button', 'data-gfw-service-complete': '' });
    view.completion.hidden = true;
    view.completion.addEventListener('click', async () => {
      if (current !== view || view.busy || !view.resume) return;
      lock(view, true);
      try { await view.resume(); if (current === view) close(root); }
      catch (error) { feedback(view, error.message || 'Completion is still unavailable. The service was not repeated.', true); }
      finally { if (current === view) lock(view, false); }
    });
    view.actions.append(view.completion);
    dialog.addEventListener('cancel', event => { event.preventDefault(); cancel(view); });
    dialog.addEventListener('keydown', event => {
      if (event.key === 'Escape') { event.preventDefault(); cancel(view); }
      if (event.key !== 'Tab') return;
      const controls = Array.from(dialog.querySelectorAll('button, textarea, audio')).filter(control => !control.disabled && !control.hidden);
      const first = controls[0] || heading;
      const last = controls.at(-1) || heading;
      if (event.shiftKey && (document.activeElement === first || document.activeElement === heading)) { event.preventDefault(); last.focus(); }
      else if (!event.shiftKey && document.activeElement === last) { event.preventDefault(); first.focus(); }
    });
    dialog.addEventListener('click', event => { if (event.target === dialog && !view.busy) cancel(view); });
    (root.querySelector('[data-gfm-platform]') || root).append(dialog);
    try {
      if (typeof dialog.showModal === 'function') dialog.showModal(); else dialog.setAttribute('open', '');
      heading.focus({ preventScroll: true });
    } catch (error) { close(root, false); throw error; }
  };
  const releasePlayback = (root, disconnectedOnly = false, retain = false) => {
    for (const player of playbackSources.keys()) {
      if (disconnectedOnly && player.isConnected) continue;
      if (root && !root.contains(player) && player.isConnected) continue;
      player.pause?.(); player.removeAttribute('src'); player.load?.();
      const url = playback.get(player);
      if (url) URL.revokeObjectURL(url);
      playback.delete(player);
      if (!retain) playbackSources.delete(player);
    }
  };
  const attachPlayback = (container, attachment) => {
    const audio = attachment?.audio;
    if (!audio || !Number.isInteger(audio.durationMillis) || audio.durationMillis < 1 || audio.durationMillis > 60000) return;
    try {
      const bytes = audioBytes(audio.file);
      const url = URL.createObjectURL(new Blob([bytes], { type: audio.file.mediaType }));
      const player = element('audio', '', { controls: '', preload: 'none', 'data-gfw-audio-playback': attachment.id, 'aria-label': 'Play ' + attachment.name });
      player.src = url;
      player.addEventListener('play', () => {
        for (const other of playback.keys()) if (other !== player) other.pause?.();
      });
      playback.set(player, url);
      playbackSources.set(player, { bytes, mediaType: audio.file.mediaType });
      container.append(player);
    } catch (_) {}
  };
  const interrupt = () => {
    releasePlayback(null, false, true);
    if (current?.kind === 'audio' && !current.host && !current.sent) cancel(current);
  };
  const resumePlayback = () => {
    for (const [player, source] of playbackSources) {
      if (!player.isConnected || playback.has(player)) continue;
      const url = URL.createObjectURL(new Blob([source.bytes], { type: source.mediaType }));
      playback.set(player, url); player.src = url;
    }
  };
  document.addEventListener('visibilitychange', () => { if (document.hidden) interrupt(); else resumePlayback(); });
  window.addEventListener('pagehide', interrupt);
  window.addEventListener('pageshow', resumePlayback);
  new MutationObserver(() => {
    releasePlayback(null, true);
    if (current && !current.root.isConnected) close(current.root, false);
  }).observe(document.documentElement, { childList: true, subtree: true });
  document.addEventListener('focusin', event => {
    if (current && !current.dialog.contains(event.target)) current.heading.focus({ preventScroll: true });
  });
  window.addEventListener('beforeunload', event => {
    if (!current) return;
    event.preventDefault(); event.returnValue = '';
  });
  window.navigation?.addEventListener('navigate', event => {
    if (current && event.cancelable && !current.downloadURLs.has(event.destination?.url)) event.preventDefault();
  });
  window.__gofastr_services = { open, close, capabilities, attachPlayback, releasePlayback, has: root => !!current && (!root || current.root === root) };
  if (window.__gofastr) (window.__gofastr.loadedModules ||= {})['gfm-webview-services'] = true;
})();
