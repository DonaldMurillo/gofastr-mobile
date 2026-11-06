(() => {
  'use strict';
  const namePattern = /^[a-z][a-z0-9.-]{0,79}\/v[1-9][0-9]{0,2}$/;
  const adapters = names => {
    if (!Array.isArray(names) || new Set(names).size !== names.length) throw new Error('Invalid selected component extensions.');
    const definitions = window.__gofastr_component_extension_definitions || [];
    if (!Array.isArray(definitions)) throw new Error('Invalid compiled component registrations.');
    return new Map(names.map(name => {
      const matches = definitions.filter(item => item?.name === name);
      const adapter = matches[0];
      if (!namePattern.test(name) || matches.length !== 1 || typeof adapter.mount !== 'function' || typeof adapter.capabilities !== 'function') {
        throw new Error('Required rendering adapter is missing or invalid: ' + name);
      }
      const facts = adapter.capabilities();
      if (!facts || Object.keys(facts).length !== 1 || !Object.hasOwn(facts, name) || !['available', 'fallback', 'unavailable'].includes(facts[name])) {
        throw new Error('Invalid rendering adapter capabilities: ' + name);
      }
      return [name, { adapter, support: facts[name] }];
    }));
  };
  const capabilities = (names = []) => Object.fromEntries(Array.from(adapters(names), ([name, entry]) => [name, entry.support]));
  const create = root => {
    const entries = new Map();
    let disposed = false;
    let open;
    const release = entry => {
      entry.active = false;
      try { entry.instance?.dispose(); } catch (_) {}
      entry.container.remove();
    };
    const reconcile = (snapshots = [], options = {}) => {
      if (disposed) return;
      if (!Array.isArray(snapshots)) throw new Error('Invalid component snapshots.');
      const selected = adapters(options.names || []);
      const identifiers = new Set();
      for (const snapshot of snapshots) {
        if (!snapshot || typeof snapshot.id !== 'string' || !/^[A-Za-z0-9_.:-]{1,128}$/.test(snapshot.id) || ['.', '..'].includes(snapshot.id) || identifiers.has(snapshot.id) ||
            typeof snapshot.payload !== 'string' || new TextEncoder().encode(snapshot.payload).length > 2 * 1024 * 1024) {
          throw new Error('Invalid or oversized component snapshot.');
        }
        identifiers.add(snapshot.id);
      }
      open = options.open;
      for (const [identifier, entry] of entries) {
        const snapshot = snapshots.find(item => item.id === identifier);
        if (!snapshot || snapshot.name !== entry.snapshot.name) { release(entry); entries.delete(identifier); }
      }
      let group = root.querySelector('[data-gfw-components]');
      if (!group) {
        group = document.createElement('div');
        group.setAttribute('data-gfw-components', '');
        (root.querySelector('[data-gfw-form]') || root).append(group);
      }
      const containers = [];
      let failure;
      for (const snapshot of snapshots) {
        let entry = entries.get(snapshot.id);
        if (!entry) {
          const container = document.createElement('div');
          container.setAttribute('data-gfw-component', snapshot.id);
          container.setAttribute('data-gfw-component-name', snapshot.name);
          entry = { container, snapshot, active: true };
          entries.set(snapshot.id, entry);
        }
        containers.push(entry.container);
        entry.snapshot = snapshot;
        try {
          const selectedAdapter = selected.get(snapshot.name);
          if (!selectedAdapter || selectedAdapter.support !== 'available') throw new Error('Required rendering adapter is unavailable: ' + snapshot.name);
          if (entry.instance) entry.instance.update(snapshot);
          else {
            entry.active = true;
            const generation = entry.generation = (entry.generation || 0) + 1;
            entry.instance = selectedAdapter.adapter.mount(entry.container, snapshot, { open: key => {
              if (!entry.active || entry.generation !== generation || disposed || !root.isConnected || typeof key !== 'string' ||
                  !Object.hasOwn(entry.snapshot.destinations || {}, key)) return;
              return open?.(entry.snapshot, key);
            } });
            if (typeof entry.instance?.update !== 'function' || typeof entry.instance?.dispose !== 'function') {
              throw new Error('Invalid mounted rendering adapter: ' + snapshot.name);
            }
          }
          entry.container.removeAttribute('data-gfw-component-error');
          entry.container.removeAttribute('role');
        } catch (error) {
          entry.active = false;
          try { entry.instance?.dispose?.(); } catch (_) {}
          entry.instance = undefined;
          entry.container.replaceChildren();
          entry.container.setAttribute('data-gfw-component-error', '');
          entry.container.setAttribute('role', 'alert');
          entry.container.textContent = 'Component unavailable: ' + snapshot.name;
          failure ||= error;
        }
      }
      if (group.children.length !== containers.length || containers.some((container, index) => group.children[index] !== container)) {
        for (const child of Array.from(group.children)) if (!containers.includes(child)) child.remove();
        containers.forEach((container, index) => { if (group.children[index] !== container) group.insertBefore(container, group.children[index] || null); });
      }
      if (failure) throw failure;
    };
    return { reconcile, dispose() {
      if (disposed) return;
      disposed = true;
      for (const entry of entries.values()) release(entry);
      entries.clear();
    } };
  };
  window.__gofastr_rendering = { capabilities, create };
  if (window.__gofastr) (window.__gofastr.loadedModules ||= {})['gfm-webview-rendering'] = true;
})();
