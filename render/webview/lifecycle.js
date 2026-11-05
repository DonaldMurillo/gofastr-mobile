(() => {
  'use strict';
  const createHistory = ({ required, authorize, report }) => {
    const runtime = window.__gofastr;
    const previous = history.state?.gfwHistory;
    const restored = typeof previous?.session === 'string' && /^[a-zA-Z0-9-]{1,64}$/.test(previous.session) &&
      Number.isSafeInteger(previous.position) && previous.position >= 0;
    const session = restored ? previous.session : crypto.randomUUID();
    let position = restored ? previous.position : 0;
    let pending;
    let approved;
    let timer;
    const url = () => location.pathname + location.search + location.hash;
    const read = () => ({ url: url(), state: history.state, tag: history.state?.gfwHistory });
    const usable = entry => entry?.tag?.session === session && Number.isSafeInteger(entry.tag.position);
    const matches = (first, second) => usable(first) && usable(second) && first.url === second.url && first.tag.position === second.tag.position;
    const tag = next => {
      position = next;
      history.replaceState({ ...history.state, gfwHistory: { session, position } }, '', location.href);
    };
    tag(position);
    let here = read();
    const originalPush = runtime?._pushURL;
    const push = function (...args) {
      const left = read();
      const result = originalPush.apply(this, args);
      if (!pending) {
        tag(usable(left) ? left.tag.position + (args[1]?.replace ? 0 : 1) : ++position);
        here = read();
      }
      return result;
    };
    if (typeof originalPush === 'function') runtime._pushURL = push;
    const stopTimer = () => { clearTimeout(timer); timer = undefined; };
    const fail = message => {
      stopTimer();
      const source = pending?.source || here;
      pending = approved = undefined;
      history.replaceState(source.state, '', source.url);
      here = read();
      report(message);
    };
    const move = target => {
      const current = read();
      if (!usable(current) || !usable(target)) {
        fail('This history entry cannot be safely restored. Your screen is unchanged; use its navigation controls.');
        return;
      }
      const distance = target.tag.position - current.tag.position;
      if (!distance) {
        fail('History changed while leaving. Your screen is unchanged; try its navigation controls.');
        return;
      }
      stopTimer();
      timer = setTimeout(() => fail('History restoration did not finish. Your screen and unsaved text are still here.'), 2000);
      history.go(distance);
    };
    const resume = transaction => {
      if (pending !== transaction || !matches(read(), transaction.source) || transaction.waiting) return;
      if (!transaction.started) {
        transaction.started = true;
        transaction.waiting = true;
        Promise.resolve().then(authorize).then(permit => {
          transaction.waiting = false;
          transaction.permit = permit;
          transaction.finished = true;
          resume(transaction);
        }, error => {
          transaction.waiting = false;
          transaction.finished = true;
          report(error.message || 'Could not verify that the draft is safe. Stay here and try again.');
          resume(transaction);
        });
        return;
      }
      if (!transaction.finished) return;
      if (transaction.cancelled || typeof transaction.permit !== 'function' || !transaction.permit()) {
        pending = undefined;
        return;
      }
      transaction.replaying = true;
      move(transaction.destination);
    };
    const pop = () => {
      const destination = read();
      if (approved) {
        const permission = approved;
        approved = undefined; stopTimer();
        if (matches(here, permission.source) && usable(destination) &&
            destination.tag.position === permission.source.tag.position + permission.distance && permission.permit()) {
          here = destination;
          return false;
        }
      }
      if (pending) {
        const transaction = pending;
        if (transaction.replaying && matches(destination, transaction.destination) && transaction.permit()) {
          stopTimer(); pending = undefined; here = destination;
          return false;
        }
        if (matches(destination, transaction.source)) {
          stopTimer();
          resume(transaction);
        } else {
          transaction.cancelled = true;
          transaction.replaying = false;
          move(transaction.source);
        }
        return true;
      }
      if (matches(destination, here) || !required()) { here = destination; return false; }
      pending = { source: here, destination };
      if (!usable(destination) || !usable(here)) {
        fail('This history entry cannot be safely restored. Your screen is unchanged; use its navigation controls.');
        return true;
      }
      move(here);
      return true;
    };
    const descriptor = Object.getOwnPropertyDescriptor(runtime, '_interceptPopstate');
    let inner = descriptor?.value;
    const delegated = () => descriptor?.get ? descriptor.get.call(runtime) : inner;
    const guarded = () => pop() || (delegated()?.() ?? false);
    const get = () => guarded;
    Object.defineProperty(runtime, '_interceptPopstate', {
      configurable: true, get,
      set(next) { if (descriptor?.set) descriptor.set.call(runtime, next); else inner = next; },
    });
    const commit = () => { if (!pending) here = read(); };
    window.addEventListener('gofastr:navigate', commit);
    return {
      get pending() { return !!pending || !!approved; },
      traverse(distance, permit) {
        if (pending || approved || !Number.isSafeInteger(distance) || !distance || typeof permit !== 'function' || !permit()) return false;
        approved = { source: here, distance, permit };
        timer = setTimeout(() => { approved = undefined; timer = undefined; }, 2000);
        history.go(distance);
        return true;
      },
      close() {
        stopTimer(); pending = approved = undefined;
        window.removeEventListener('gofastr:navigate', commit);
        if (runtime?._pushURL === push) runtime._pushURL = originalPush;
        if (Object.getOwnPropertyDescriptor(runtime, '_interceptPopstate')?.get === get) {
          if (descriptor?.get) Object.defineProperty(runtime, '_interceptPopstate', descriptor);
          else if (descriptor) Object.defineProperty(runtime, '_interceptPopstate', { ...descriptor, value: inner });
          else { delete runtime._interceptPopstate; if (inner) runtime._interceptPopstate = inner; }
        }
      },
    };
  };
  window.__gofastr_lifecycle = { createHistory };
  if (window.__gofastr) (window.__gofastr.loadedModules ||= {})['gfm-webview-lifecycle'] = true;
})();
