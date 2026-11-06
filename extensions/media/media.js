(() => {
  'use strict';
  const name = 'gofastr.media/v1';
  const players = new Set();
  const stylesheet = `
    [data-gfw-media] { min-width: 0; }
    [data-gfw-media] [hidden] { display: none !important; }
    [data-gfw-media] h2 { margin: 0 0 .75rem; font-size: .875rem; font-weight: 600; color: var(--color-text-muted); }
    [data-gfw-media] .gfw-images { display: grid; gap: .75rem; margin-block: 1rem; }
    [data-gfw-media] [data-gfw-image] { display: block; min-width: 0; margin: 0; border-radius: .75rem; overflow: hidden; }
    [data-gfw-media] img { display: block; width: 100%; height: auto; object-fit: contain; }
    [data-gfw-media] .gfw-attachments { margin-block: 1rem; }
    [data-gfw-media] ul { list-style: none; margin: 0; padding: 0; display: grid; gap: .5rem; }
    [data-gfw-media] .gfw-attachments a { display: flex; align-items: center; gap: .875rem; min-height: var(--gfm-target, 44px); padding: 1rem; border-radius: 1rem; color: var(--color-text); background: var(--color-surface-soft); overflow-wrap: anywhere; }
    [data-gfw-media] a[role="link"] { cursor: pointer; }
    [data-gfw-media] a:focus-visible { outline: 2px solid var(--color-primary); outline-offset: 3px; }
    [data-gfw-media] .gfw-file-copy { display: grid; gap: .25rem; min-width: 0; }
    [data-gfw-media] .gfw-file-copy span { color: var(--color-text-muted); font-size: .875rem; }
    [data-gfw-media] .gfw-file-icon { display: grid; place-items: center; flex: 0 0 44px; width: 44px; height: 44px; border-radius: .625rem; color: var(--color-primary); background: var(--color-surface); }
    [data-gfw-media] .gfw-file-icon svg { width: 24px; height: 24px; }
    [data-gfw-media] audio { display: block; width: 100%; margin-block: .5rem; }
    [data-gfw-media] .gfw-file-preview { padding: 1.5rem var(--gfm-gutter, 1rem); }
    [data-gfw-media] .gfw-file-heading { display: flex; flex-direction: column; align-items: center; gap: .75rem; text-align: center; margin-bottom: 2rem; }
    [data-gfw-media] .gfw-file-heading h1 { font-size: 1.5rem; margin: .25rem 0 0; overflow-wrap: anywhere; }
    [data-gfw-media] .gfw-file-description, [data-gfw-media] .gfw-file-caption { margin: 0; color: var(--color-text-muted); }
    [data-gfw-media] .gfw-file-text { border-top: 1px solid var(--color-border); padding-top: 1.5rem; }
    [data-gfw-media] pre { margin: 0; white-space: pre-wrap; overflow-wrap: anywhere; font: inherit; line-height: 1.7; }
    [data-gfw-form-style="document"] [data-gfw-media] .gfw-images { grid-template-columns: repeat(2, minmax(0, 1fr)); }
    [data-gfw-form-style="document"] [data-gfw-media] .gfw-images h2 { grid-column: 1 / -1; }
    [data-gfw-form-style="document"] [data-gfw-media] img { height: 10rem; object-fit: cover; }
  `;
  const element = (tag, text = '', attributes = {}) => {
    const node = document.createElement(tag);
    node.textContent = text;
    for (const [key, value] of Object.entries(attributes)) node.setAttribute(key, value);
    return node;
  };
  const bytes = (data, maximum) => {
    if (typeof data !== 'string' || !data || data.length > 4 * Math.ceil(maximum / 3) || data.length % 4 !== 0 || !/^[A-Za-z0-9+/]+={0,2}$/.test(data)) throw new Error('Invalid private media bytes.');
    const raw = atob(data);
    if (!raw.length || raw.length > maximum) throw new Error('Oversized private media bytes.');
    return Uint8Array.from(raw, character => character.charCodeAt(0));
  };
  const metadata = file => {
    if (!file || typeof file.id !== 'string' || !file.id || file.id.length > 128 || typeof file.name !== 'string' || !file.name || file.name.length > 200 ||
        /[/\\:\u0000-\u001f\u007f]/.test(file.name) || typeof file.mediaType !== 'string' ||
        !/^[a-z0-9!#$&^_.+-]+\/[a-z0-9!#$&^_.+-]+(?:;[^\r\n]*)?$/i.test(file.mediaType) || !Number.isSafeInteger(file.size) || file.size < 0 || file.size > 1024 * 1024) throw new Error('Invalid media metadata.');
  };
  const audioSource = file => {
    if (!file.audio) return null;
    const audio = file.audio;
    if (!Number.isInteger(audio.durationMillis) || audio.durationMillis < 1 || audio.durationMillis > 60000 ||
        audio.file?.name !== file.name || audio.file?.mediaType !== file.mediaType) throw new Error('Invalid private audio metadata.');
    const content = bytes(audio.file.data, 1024 * 1024);
    const text = (offset, length) => String.fromCharCode(...content.slice(offset, offset + length));
    const type = file.mediaType.split(';')[0].toLowerCase();
    const valid = type === 'audio/webm' ? content.length >= 16 && content[0] === 26 && content[1] === 69 && content[2] === 223 && content[3] === 163 :
      type === 'audio/ogg' ? content.length >= 32 && text(0, 4) === 'OggS' :
      ['audio/mp4', 'audio/m4a', 'audio/x-m4a'].includes(type) ? content.length >= 16 && text(4, 4) === 'ftyp' :
      type === 'audio/aac' ? content.length >= 7 && content[0] === 255 && (content[1] & 246) === 240 :
      ['audio/wav', 'audio/wave', 'audio/x-wav'].includes(type) ? content.length >= 44 && text(0, 4) === 'RIFF' && text(8, 4) === 'WAVE' : false;
    if (!valid || content.length !== file.size) throw new Error('Invalid private audio content.');
    return { bytes: content, mediaType: file.mediaType };
  };
  const parse = component => {
    if (component?.name !== name || typeof component.payload !== 'string' || new TextEncoder().encode(component.payload).length > 2 * 1024 * 1024) throw new Error('Invalid media component.');
    const content = JSON.parse(component.payload);
    if (!content || Array.isArray(content) || typeof content !== 'object' || Object.keys(content).some(key => !['images', 'attachments', 'preview'].includes(key))) throw new Error('Invalid media payload.');
    const images = content.images ?? [], attachments = content.attachments ?? [];
    if (!Array.isArray(images) || !Array.isArray(attachments) || images.length + attachments.length > 64 || (!images.length && !attachments.length && !content.preview)) throw new Error('Invalid media collection.');
    const imageIDs = new Set(), fileIDs = new Set();
    for (const image of images) {
      if (!image || typeof image.id !== 'string' || !image.id || imageIDs.has(image.id) || typeof image.alt !== 'string' || !image.alt ||
          !Number.isInteger(image.width) || !Number.isInteger(image.height) || image.width < 1 || image.height < 1 || image.width > 1024 || image.height > 1024) throw new Error('Invalid media image.');
      const content = bytes(image.data, 128 * 1024);
      if (content[0] !== 255 || content[1] !== 216 || content[2] !== 255) throw new Error('Media thumbnails must contain JPEG bytes.');
      imageIDs.add(image.id);
    }
    const audio = new Map();
    for (const file of attachments) {
      metadata(file);
      if (fileIDs.has(file.id)) throw new Error('Duplicate media attachment.');
      fileIDs.add(file.id);
      audio.set(file.id, audioSource(file));
    }
    if (content.preview) {
      const preview = content.preview;
      metadata(preview.file);
      if (preview.file.audio || (preview.text != null && (typeof preview.text !== 'string' || new TextEncoder().encode(preview.text).length > 16384 || preview.text.includes('\u0000'))) ||
          (preview.caption != null && (typeof preview.caption !== 'string' || preview.caption.length > 4096)) ||
          (preview.previewTitle != null && (typeof preview.previewTitle !== 'string' || preview.previewTitle.length > 200))) throw new Error('Invalid media text preview.');
    }
    return { images, attachments, preview: content.preview, audio };
  };
  const category = type => type.startsWith('image/') ? 'Photo' : type.startsWith('audio/') ? 'Audio' : type === 'application/pdf' ? 'PDF document' :
    ['text/markdown', 'text/x-markdown'].includes(type) ? 'Markdown' : type.startsWith('text/') ? 'Text document' : 'File';
  const description = file => category(file.mediaType) + ' · ' + (file.size >= 1024 * 1024 ? (file.size / (1024 * 1024)).toFixed(1) + ' MB' : file.size >= 1024 ? Math.round(file.size / 1024) + ' KB' : file.size + ' B');
  const icon = file => {
    const kind = file.mediaType.startsWith('image/') ? 'photo' : file.mediaType.startsWith('audio/') ? 'audio' : 'file';
    const node = element('span', '', { class: 'gfw-file-icon', 'data-gfw-file-icon': kind, 'aria-hidden': 'true' });
    const svg = document.createElementNS('http://www.w3.org/2000/svg', 'svg');
    for (const [key, value] of Object.entries({ viewBox: '0 0 24 24', fill: 'none', stroke: 'currentColor', 'stroke-width': '1.5', 'stroke-linecap': 'round', 'stroke-linejoin': 'round' })) svg.setAttribute(key, value);
    const path = document.createElementNS('http://www.w3.org/2000/svg', 'path');
    path.setAttribute('d', kind === 'photo' ? 'M4 3h16v18H4zM4 16l5-5 4 4 3-3 4 4M16 7h.01' : kind === 'audio' ? 'M3 10v4m4-8v12m5-16v20m5-16v12m4-8v4' : 'M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8l-6-6Zm0 0v6h6M8 13h8M8 17h6');
    svg.append(path); node.append(svg);
    return node;
  };
  const ordered = (parent, children) => {
    for (const child of Array.from(parent.children)) if (!children.includes(child)) child.remove();
    children.forEach((child, index) => { if (parent.children[index] !== child) parent.insertBefore(child, parent.children[index] || null); });
  };
  const mount = (container, component, { open }) => {
    let disposed = false, snapshot, lastPayload, content, previewIcon, previewType;
    const imageRows = new Map(), fileRows = new Map();
    const style = element('style', stylesheet);
    const gallery = element('section', '', { class: 'gfw-images', 'data-gfw-images': '', 'aria-label': 'Photos' });
    const imageHeading = element('h2', 'Photos'); gallery.append(imageHeading);
    const attachments = element('section', '', { class: 'gfw-attachments', 'data-gfw-attachments': '', 'aria-label': 'File attachments' });
    const list = element('ul'); attachments.append(element('h2', 'Attachments'), list);
    const preview = element('article', '', { class: 'gfw-file-preview', 'data-gfw-file-preview': '' });
    const heading = element('header', '', { class: 'gfw-file-heading' });
    const title = element('a', '', { 'data-gfw-preview': '' }), titleText = element('h1'); title.append(titleText);
    const detail = element('p', '', { class: 'gfw-file-description' }), caption = element('p', '', { class: 'gfw-file-caption' });
    heading.append(title, detail, caption);
    const textSection = element('section', '', { class: 'gfw-file-text' }), textTitle = element('h2'), text = element('pre'); textSection.append(textTitle, text);
    preview.append(heading, textSection);
    container.setAttribute('data-gfw-media', '');
    container.replaceChildren(style, gallery, attachments, preview);
    const link = (node, key) => {
      node.eventKey = key;
      const enabled = Object.hasOwn(snapshot.destinations || {}, key);
      if (enabled) { node.setAttribute('role', 'link'); node.setAttribute('tabindex', '0'); }
      else { node.removeAttribute('role'); node.removeAttribute('tabindex'); }
    };
    const bind = node => {
      const activate = event => {
        if (event.defaultPrevented || (event.type === 'click' && (event.button !== 0 || event.metaKey || event.ctrlKey || event.shiftKey || event.altKey)) || disposed ||
            !Object.hasOwn(snapshot.destinations || {}, node.eventKey)) return;
        event.preventDefault();
        void Promise.resolve(open(node.eventKey)).catch(() => {});
      };
      node.addEventListener('click', activate);
      node.addEventListener('keydown', event => { if (event.key === 'Enter' && !event.isComposing) activate(event); });
    };
    bind(title);
    const releaseAudio = row => {
      if (!row.player) return;
      row.player.pause(); row.player.removeAttribute('src'); row.player.load();
      if (row.url) URL.revokeObjectURL(row.url);
      row.url = undefined;
    };
    const restoreAudio = row => {
      if (disposed || document.hidden || !row.player || row.url || !row.source) return;
      row.url = URL.createObjectURL(new Blob([row.source.bytes], { type: row.source.mediaType }));
      row.player.src = row.url;
    };
    const removeAudio = row => {
      releaseAudio(row);
      if (row.player) { players.delete(row.player); row.player.remove(); }
      row.player = row.source = undefined;
    };
    const interrupt = () => { for (const row of fileRows.values()) releaseAudio(row); };
    const resume = () => { if (container.isConnected) for (const row of fileRows.values()) restoreAudio(row); };
    const visibility = () => { if (document.hidden) interrupt(); else resume(); };
    const update = next => {
      if (disposed) return;
      if (next.payload !== lastPayload) content = parse(next);
      snapshot = next;
      if (content.preview) {
        ordered(preview, [heading, gallery, attachments, textSection]);
        ordered(container, [style, preview]);
      } else {
        ordered(container, [style, gallery, attachments, preview]);
        ordered(preview, [heading, textSection]);
      }
      for (const [identifier, row] of imageRows) if (!content.images.some(image => image.id === identifier)) { row.node.remove(); imageRows.delete(identifier); }
      const images = content.images.map((image, index) => {
        let row = imageRows.get(image.id);
        if (!row) {
          row = { node: element('a', '', { 'data-gfw-image': image.id }), image: element('img') };
          row.node.append(row.image); bind(row.node); imageRows.set(image.id, row);
        }
        link(row.node, 'image.' + index);
        row.node.setAttribute('aria-label', 'Open photo: ' + image.alt);
        row.image.alt = image.alt; row.image.width = image.width; row.image.height = image.height;
        row.image.decoding = 'async'; row.image.loading = 'lazy';
        if (row.data !== image.data) { row.image.src = 'data:image/jpeg;base64,' + image.data; row.data = image.data; }
        return row.node;
      });
      ordered(gallery, [imageHeading, ...images]); gallery.hidden = images.length === 0;
      for (const [identifier, row] of fileRows) if (!content.attachments.some(file => file.id === identifier)) { removeAudio(row); row.node.remove(); fileRows.delete(identifier); }
      const rows = content.attachments.map((file, index) => {
        let row = fileRows.get(file.id);
        if (!row) {
          row = { node: element('li'), link: element('a', '', { 'data-gfw-attachment': file.id }), title: element('strong'), detail: element('span') };
          row.icon = icon(file); row.mediaType = file.mediaType;
          const copy = element('div', '', { class: 'gfw-file-copy' }); copy.append(row.title, row.detail); row.link.append(row.icon, copy); row.node.append(row.link);
          bind(row.link); fileRows.set(file.id, row);
        }
        link(row.link, 'file.' + index);
        row.title.textContent = file.name; row.detail.textContent = description(file);
        if (row.mediaType !== file.mediaType) {
          row.icon.remove(); row.icon = icon(file); row.mediaType = file.mediaType;
          row.link.insertBefore(row.icon, row.link.children[0] || null);
        }
        const audio = JSON.stringify(file.audio || null);
        if (row.audio !== audio) {
          removeAudio(row); row.audio = audio; row.source = content.audio.get(file.id);
          if (row.source) {
            row.player = element('audio', '', { controls: '', preload: 'none', 'data-gfw-audio-playback': file.id });
            row.player.addEventListener('play', () => { for (const other of players) if (other !== row.player) other.pause(); });
            players.add(row.player); row.node.append(row.player); restoreAudio(row);
          }
        }
        row.player?.setAttribute('aria-label', 'Play ' + file.name);
        return row.node;
      });
      ordered(list, rows); attachments.hidden = rows.length === 0;
      preview.hidden = !content.preview;
      if (content.preview) {
        const value = content.preview;
        titleText.textContent = value.file.name; detail.textContent = description(value.file); caption.textContent = value.caption || '';
        link(title, 'preview');
        if (previewType !== value.file.mediaType) {
          previewIcon?.remove(); previewIcon = icon(value.file); previewType = value.file.mediaType;
          heading.insertBefore(previewIcon, title);
        }
        textTitle.textContent = value.previewTitle || 'Text preview'; text.textContent = value.text || ''; textSection.hidden = !value.text;
      }
      lastPayload = next.payload;
    };
    try { update(component); }
    catch (error) { for (const row of fileRows.values()) removeAudio(row); throw error; }
    document.addEventListener('visibilitychange', visibility);
    window.addEventListener('pagehide', interrupt);
    window.addEventListener('pageshow', resume);
    return { update, dispose() {
      if (disposed) return;
      disposed = true;
      for (const row of fileRows.values()) removeAudio(row);
      fileRows.clear(); imageRows.clear();
      document.removeEventListener('visibilitychange', visibility);
      window.removeEventListener('pagehide', interrupt);
      window.removeEventListener('pageshow', resume);
      container.replaceChildren(); container.removeAttribute('data-gfw-media');
    } };
  };
  (window.__gofastr_component_extension_definitions ||= []).push({ name, capabilities: () => ({ [name]: 'available' }), mount });
})();
