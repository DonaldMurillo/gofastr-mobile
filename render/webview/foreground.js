(() => {
  'use strict';
  if (window.__gofastr_foreground) return;
  let owner;
  window.__gofastr_foreground = {
    claim(candidate) {
      if (owner && owner !== candidate) throw new Error('Finish or cancel the open foreground task first.');
      owner = candidate;
    },
    release(candidate) { if (owner === candidate) owner = null; },
    has: () => !!owner,
  };
  if (window.__gofastr) (window.__gofastr.loadedModules ||= {})['gfm-webview-foreground'] = true;
})();
