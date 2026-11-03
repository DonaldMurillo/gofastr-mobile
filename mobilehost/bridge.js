// gofastr-mobile bridge: carries the page's requests to the in-process Go
// app over the native shell's message channel instead of the WebView's
// request interception, which drops what this channel keeps: Android's
// WebResourceRequest has no body at all, WKURLSchemeHandler drops Blob
// and File bodies, and neither can hand a redirect back to the page.
//
// Two pieces, both standard extension points:
//
//   window.__gofastr_transport  the GoFastr runtime's host transport.
//     Every runtime fetch looks it up at call time (core-ui/runtime,
//     kernel.js __gofastr.fetch), so installing it here, from a deferred
//     rail script, covers every request a reader can cause. Boot-time
//     GETs that ran before it are plain fetches the interception serves.
//   a window "submit" listener  a native same-origin POST form the
//     runtime left alone (no default prevented) is sent the same way, and
//     the shell shows the answer as the page's next document.
//
// The channel is one of:
//   iOS      window.webkit.messageHandlers.gofastr, a
//            WKScriptMessageHandlerWithReply: postMessage returns a
//            Promise of the reply string.
//   Android  a MessagePort the shell posts to the main frame with
//            WebView.postWebMessage(..., appOrigin) (frame- and
//            origin-restricted, unlike addJavascriptInterface). The sim
//            delivers the same shape.
// Both carry JSON strings; mobilehost.Host.Bridge is the other end.
(() => {
  'use strict';
  if (window.__gofastr_transport) return;

  const HELLO = 'gofastr-mobile-bridge';
  const PORT_WAIT_MS = 10000;

  // ---- channel -----------------------------------------------------------
  const ios = window.webkit && window.webkit.messageHandlers && window.webkit.messageHandlers.gofastr;
  let port = null;
  let seq = 0;
  const pending = new Map();
  const waiters = [];
  if (!ios) {
    window.addEventListener('message', (e) => {
      // The shell's hello carries the port; take the first one only.
      if (port || e.data !== HELLO || !e.ports || !e.ports[0]) return;
      port = e.ports[0];
      port.onmessage = (m) => {
        let reply;
        try { reply = JSON.parse(m.data); } catch (_) { return; }
        const settle = pending.get(reply.id);
        if (settle) { pending.delete(reply.id); settle(reply); }
      };
      while (waiters.length) waiters.shift()();
    });
  }
  const portReady = () => port ? Promise.resolve() : new Promise((resolve, reject) => {
    const t = setTimeout(() => reject(new TypeError('gofastr-mobile: no native bridge')), PORT_WAIT_MS);
    waiters.push(() => { clearTimeout(t); resolve(); });
  });
  const send = async (msg) => {
    if (ios) return JSON.parse(await ios.postMessage(JSON.stringify(msg)));
    await portReady();
    const id = 'r' + (++seq);
    return new Promise((resolve) => {
      pending.set(id, resolve);
      port.postMessage(JSON.stringify(Object.assign({ id }, msg)));
    });
  };

  // ---- encoding ------------------------------------------------------------
  const enc = (u8) => {
    let s = '';
    for (let i = 0; i < u8.length; i += 0x8000) s += String.fromCharCode.apply(null, u8.subarray(i, i + 0x8000));
    return btoa(s);
  };
  const dec = (s) => {
    const bin = atob(s || '');
    const u8 = new Uint8Array(bin.length);
    for (let i = 0; i < bin.length; i++) u8[i] = bin.charCodeAt(i);
    return u8;
  };
  // Compare scheme + host, never .origin: a custom scheme (iOS's
  // gofastr://app) has the opaque origin "null", which every other
  // opaque URL shares.
  const ours = (u) => u.protocol === location.protocol && u.host === location.host;

  const toMessage = async (req, navigate) => {
    const msg = { method: req.method, url: req.url, headers: [...req.headers] };
    if (req.method !== 'GET' && req.method !== 'HEAD') {
      const buf = new Uint8Array(await req.arrayBuffer());
      if (buf.length) msg.body = enc(buf);
    }
    if (navigate) msg.navigate = true;
    return msg;
  };

  const NULL_BODY = new Set([101, 103, 204, 205, 304]);
  const toResponse = (r) => {
    const resp = new Response(NULL_BODY.has(r.status) ? null : dec(r.body), { status: r.status, headers: r.headers || [] });
    // fetch's own fields the runtime reads: a followed 3xx is another
    // URL's answer, never the request's (core-ui ARCHITECTURE.md,
    // "Server redirects").
    Object.defineProperty(resp, 'url', { value: r.url });
    Object.defineProperty(resp, 'redirected', { value: !!r.redirected });
    return resp;
  };

  // ---- the runtime's host transport ----------------------------------------
  async function transport(input, init) {
    const req = new Request(input, init);
    // req, not (input, init): new Request(input) moved input's body.
    if (!ours(new URL(req.url))) return fetch(req);
    const sig = req.signal;
    sig.throwIfAborted();
    const msg = await toMessage(req, false);
    const reply = await new Promise((resolve, reject) => {
      const onAbort = () => reject(sig.reason);
      sig.addEventListener('abort', onAbort, { once: true });
      send(msg).then(resolve, reject).finally(() => sig.removeEventListener('abort', onAbort));
    });
    if (reply.error) throw new TypeError('gofastr-mobile: ' + reply.error);
    return toResponse(reply);
  }
  Object.defineProperty(window, '__gofastr_transport', { value: transport, writable: false, configurable: false });

  // ---- native POST forms ---------------------------------------------------
  // Registered on window, so it runs after the runtime's document-level
  // submit bridge: a form the runtime takes (data-cui-rpc, data-cui-spa,
  // a JSON enctype) arrives here with its default already prevented.
  const formAttr = (form, sub, name) => (sub && sub.hasAttribute('form' + name)) ? sub.getAttribute('form' + name) : form.getAttribute(name);
  window.addEventListener('submit', (e) => {
    const form = e.target;
    if (e.defaultPrevented || !(form instanceof HTMLFormElement)) return;
    const sub = e.submitter;
    if ((formAttr(form, sub, 'method') || 'get').toLowerCase() !== 'post') return; // GET carries no body
    const target = formAttr(form, sub, 'target');
    if (target && target.toLowerCase() !== '_self') return;
    const action = new URL(formAttr(form, sub, 'action') || location.href, document.baseURI);
    if (!ours(action)) return;
    const enctype = (formAttr(form, sub, 'enctype') || '').toLowerCase();
    const fd = new FormData(form, sub);
    let body;
    if (enctype === 'multipart/form-data') {
      body = fd;
    } else if (enctype === 'text/plain') {
      body = '';
      fd.forEach((v, k) => { body += k + '=' + (typeof v === 'string' ? v : v.name) + '\r\n'; });
    } else {
      // A native urlencoded submission sends a file control's name.
      body = new URLSearchParams();
      fd.forEach((v, k) => body.append(k, typeof v === 'string' ? v : v.name));
    }
    e.preventDefault();
    const init = { method: 'POST', body };
    if (enctype === 'text/plain') init.headers = { 'Content-Type': 'text/plain;charset=UTF-8' };
    toMessage(new Request(action, init), true)
      .then(send)
      .then((reply) => { if (reply.error) throw new Error(reply.error); })
      .catch((err) => console.error('[gofastr-mobile] form submission failed:', err && err.message));
  });
})();
