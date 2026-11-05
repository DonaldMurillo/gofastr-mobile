(() => {
  'use strict';
  if (window.__gofastr_photos) return;
  const maximumSourceBytes = 20 * 1024 * 1024;
  const maximumHeaderBytes = 256 * 1024;
  const maximumSourceDimension = 16384;
  const maximumSourcePixels = 64 * 1024 * 1024;
  const maximumImagePixels = 16 * 1024 * 1024;
  const failure = (name, message) => Object.assign(new Error(message), { name });
  const denied = () => failure('SecurityError', 'The photo exceeds the supported image limits.');
  const unsupported = () => failure('NotSupportedError', 'A supported JPEG or PNG image and JPEG canvas encoding are required.');
  const job = () => {
    const cleanups = [];
    let cancelled = false;
    return {
      check() { if (cancelled) throw failure('AbortError', 'Photo processing cancelled.'); },
      track(cleanup) { if (cancelled) cleanup(); else cleanups.push(cleanup); },
      cancel() { cancelled = true; for (const cleanup of cleanups.splice(0).reverse()) cleanup(); },
    };
  };
  const supported = () => {
    try {
      const canvas = document.createElement('canvas');
      const input = document.createElement('input');
      input.setAttribute('type', 'file');
      return typeof Blob === 'function' && typeof Blob.prototype.arrayBuffer === 'function' && typeof Blob.prototype.slice === 'function' &&
        typeof btoa === 'function' && input.type === 'file' &&
        typeof canvas.getContext === 'function' && !!canvas.getContext('2d') && typeof canvas.toBlob === 'function' &&
        (typeof createImageBitmap === 'function' || (typeof Image === 'function' && typeof URL.createObjectURL === 'function' && typeof URL.revokeObjectURL === 'function'));
    } catch (_) { return false; }
  };
  const dimensions = (width, height, limit = maximumSourcePixels) => {
    if (!Number.isSafeInteger(width) || !Number.isSafeInteger(height) || width < 1 || height < 1 ||
        width > maximumSourceDimension || height > maximumSourceDimension || width * height > limit) throw denied();
    return { width, height };
  };
  const orientation = (bytes, start, end) => {
    const data = new DataView(bytes.buffer, bytes.byteOffset, bytes.byteLength);
    if (start + 8 > end) throw unsupported();
    const little = data.getUint16(start) === 0x4949;
    if ((!little && data.getUint16(start) !== 0x4d4d) || data.getUint16(start + 2, little) !== 42) throw unsupported();
    const directory = start + data.getUint32(start + 4, little);
    if (directory < start + 8 || directory + 2 > end) throw unsupported();
    const count = data.getUint16(directory, little);
    if (directory + 2 + count * 12 > end) throw unsupported();
    for (let index = 0; index < count; index++) {
      const offset = directory + 2 + index * 12;
      if (data.getUint16(offset, little) !== 0x0112) continue;
      const value = data.getUint16(offset + 8, little);
      if (data.getUint16(offset + 2, little) !== 3 || data.getUint32(offset + 4, little) !== 1 || value < 1 || value > 8) throw unsupported();
      return value;
    }
    return 1;
  };
  const inspect = (bytes, type) => {
    const data = new DataView(bytes.buffer, bytes.byteOffset, bytes.byteLength);
    if (type === 'image/png' && bytes.length >= 33 && data.getUint32(0) === 0x89504e47 && data.getUint32(4) === 0x0d0a1a0a &&
        data.getUint32(8) === 13 && data.getUint32(12) === 0x49484452) return dimensions(data.getUint32(16), data.getUint32(20));
    if (type !== 'image/jpeg' || bytes.length < 4 || data.getUint16(0) !== 0xffd8) throw unsupported();
    let size;
    let rotate = 1;
    let offset = 2;
    let complete = false;
    while (offset + 4 <= bytes.length) {
      if (bytes[offset++] !== 0xff) throw unsupported();
      while (bytes[offset] === 0xff) offset++;
      const marker = bytes[offset++];
      if (marker === 0xda || marker === 0xd9) { complete = true; break; }
      if (marker === 0x01 || (marker >= 0xd0 && marker <= 0xd7)) continue;
      if (offset + 2 > bytes.length) throw unsupported();
      const length = data.getUint16(offset);
      const end = offset + length;
      if (length < 2 || end > bytes.length) throw unsupported();
      if ([0xc0, 0xc1, 0xc2].includes(marker)) {
        if (length < 8) throw unsupported();
        size = dimensions(data.getUint16(offset + 5), data.getUint16(offset + 3));
      }
      if (marker === 0xe1 && length >= 8 && data.getUint32(offset + 2) === 0x45786966 && data.getUint16(offset + 6) === 0) {
        rotate = orientation(bytes, offset + 8, end);
      }
      offset = end;
    }
    if (!size || !complete) throw unsupported();
    return rotate >= 5 ? { width: size.height, height: size.width } : size;
  };
  const pngSize = async (file, size, operation) => {
    let offset = 8;
    let rotate = 1;
    for (let count = 0; count < 256 && offset + 12 <= file.size; count++) {
      const header = new Uint8Array(await file.slice(offset, offset + 8).arrayBuffer());
      operation.check();
      if (header.length !== 8) throw unsupported();
      const data = new DataView(header.buffer, header.byteOffset, header.byteLength);
      const length = data.getUint32(0);
      const type = data.getUint32(4);
      if (length > file.size - offset - 12) throw unsupported();
      if (type === 0x65584966) {
        if (length > maximumHeaderBytes) throw unsupported();
        const bytes = new Uint8Array(await file.slice(offset + 8, offset + 8 + length).arrayBuffer());
        operation.check();
        if (bytes.length !== length) throw unsupported();
        rotate = orientation(bytes, 0, bytes.length);
      }
      offset += length + 12;
      if (type === 0x49454e44 && length === 0 && offset === file.size) return rotate >= 5 ? { width: size.height, height: size.width } : size;
    }
    throw unsupported();
  };
  const image = async (source, operation) => {
    const picture = new Image();
    const url = URL.createObjectURL(source);
    await new Promise((resolve, reject) => {
      operation.track(() => {
        picture.onload = picture.onerror = null;
        picture.src = '';
        URL.revokeObjectURL(url);
        reject(failure('AbortError', 'Photo processing cancelled.'));
      });
      picture.onload = resolve;
      picture.onerror = () => reject(new Error('The selected image could not be decoded.'));
      operation.check();
      picture.src = url;
    });
    operation.check();
    return { picture, ...dimensions(picture.naturalWidth, picture.naturalHeight, maximumImagePixels) };
  };
  const normalize = async (file, policy, operation = job()) => {
    try {
      operation.check();
      if (!Number.isSafeInteger(policy?.maxBytes) || policy.maxBytes < 1 || policy.maxBytes > 1048576 ||
          !Number.isSafeInteger(policy.maxDimension) || policy.maxDimension < 1 || policy.maxDimension > 2048) throw denied();
      if (!supported()) throw unsupported();
      if (!Number.isSafeInteger(file?.size) || file.size < 1 || file.size > maximumSourceBytes) throw denied();
      if (!['image/jpeg', 'image/png'].includes(file.type?.toLowerCase()) || typeof file.slice !== 'function') throw unsupported();
      const type = file.type.toLowerCase();
      const headerBytes = Math.min(file.size, type === 'image/png' ? 33 : maximumHeaderBytes);
      const header = new Uint8Array(await file.slice(0, headerBytes).arrayBuffer());
      operation.check();
      if (header.length !== headerBytes) throw denied();
      let sourceSize = inspect(header, type);
      if (type === 'image/png') sourceSize = await pngSize(file, sourceSize, operation);
      const scale = Math.min(1, policy.maxDimension / Math.max(sourceSize.width, sourceSize.height));
      const width = Math.max(1, Math.floor(sourceSize.width * scale));
      const height = Math.max(1, Math.floor(sourceSize.height * scale));
      const source = file.slice(0, file.size, type);
      let decoded;
      if (typeof createImageBitmap === 'function') {
        const bitmap = await createImageBitmap(source, { imageOrientation: 'from-image', resizeWidth: width, resizeHeight: height, resizeQuality: 'high' });
        operation.track(() => bitmap.close());
        operation.check();
        decoded = { picture: bitmap, ...dimensions(bitmap.width, bitmap.height, maximumImagePixels) };
      } else {
        dimensions(sourceSize.width, sourceSize.height, maximumImagePixels);
        decoded = await image(source, operation);
      }
      const canvas = document.createElement('canvas');
      operation.track(() => { canvas.width = canvas.height = 0; });
      const context = canvas.getContext('2d');
      if (!context) throw unsupported();
      const decodedScale = Math.min(1, policy.maxDimension / Math.max(decoded.width, decoded.height));
      let outputWidth = Math.max(1, Math.floor(decoded.width * decodedScale));
      let outputHeight = Math.max(1, Math.floor(decoded.height * decodedScale));
      for (;;) {
        operation.check();
        canvas.width = outputWidth;
        canvas.height = outputHeight;
        context.fillStyle = '#fff';
        context.fillRect(0, 0, outputWidth, outputHeight);
        context.drawImage(decoded.picture, 0, 0, outputWidth, outputHeight);
        for (const quality of [0.9, 0.65, 0.4]) {
          const blob = await new Promise((resolve) => canvas.toBlob(resolve, 'image/jpeg', quality));
          operation.check();
          if (!blob) throw new Error('JPEG encoding failed.');
          if (blob.type !== 'image/jpeg') throw unsupported();
          if (!Number.isSafeInteger(blob.size) || blob.size < 1) throw new Error('JPEG encoding produced no image.');
          if (blob.size > policy.maxBytes) continue;
          const bytes = new Uint8Array(await blob.arrayBuffer());
          operation.check();
          if (bytes.length !== blob.size || bytes.length > policy.maxBytes) throw denied();
          const outputSize = inspect(bytes.subarray(0, maximumHeaderBytes), 'image/jpeg');
          if (outputSize.width !== outputWidth || outputSize.height !== outputHeight) throw denied();
          let binary = '';
          for (let offset = 0; offset < bytes.length; offset += 32768) binary += String.fromCharCode(...bytes.subarray(offset, offset + 32768));
          return { name: 'photo.jpg', mediaType: 'image/jpeg', data: btoa(binary) };
        }
        if (outputWidth === 1 && outputHeight === 1) throw denied();
        outputWidth = Math.max(1, Math.floor(outputWidth / 2));
        outputHeight = Math.max(1, Math.floor(outputHeight / 2));
      }
    } finally { operation.cancel(); }
  };
  window.__gofastr_photos = { supported, normalize, job };
  if (window.__gofastr) (window.__gofastr.loadedModules ||= {})['gfm-webview-photos'] = true;
})();
