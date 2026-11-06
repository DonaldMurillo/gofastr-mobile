# Portable Notes workflows

These changes use Go services, shared screen definitions, and the existing file
adapter. They do not require a phone, Mac, or changes to the Java/Swift renderers.

## Backup and restore

Open Settings, then Backup and restore. Export backup includes ordinary active,
archived and trashed notes, tags, pin state, reminder fields and attachments.
Protected notes and unsaved drafts are excluded. Backup files are unencrypted;
store them somewhere private. Restoring reminder fields does not schedule OS
notifications, and restored notes do not become widget content automatically.

A `.gfnbackup` file contains a versioned gzip-compressed JSON bundle. The
existing adapters limit each transfer to 1 MiB compressed. There are additional
limits of 500 notes, 4 MiB of note text, 64 attachments totalling 16 MiB, and
24 MiB of decoded JSON. An export that exceeds a limit fails with an explanation;
it never silently drops notes or files. For smaller batches, select up to 20
notes in the collection and choose Back up selected. Individual large attachments
can still be exported through their existing file actions.

Choose backup to restore reads and validates the complete bundle before saving
anything. Review shows counts, titles, states, text snippets and attachment
counts. It displays the first 100 records and explicitly says if more are in the
file. Restore includes all records. Existing notes are preserved. Unchanged
originals and completed imports are skipped; a changed original is restored as
a separate copy. Previously restored records stay skipped after later local
edits or attachment removal.

The owner-scoped source receipt and imported text/metadata share one database
write. Attachment identities are stable, and a completion marker is written only
after every attachment succeeds. Storage errors may leave an incomplete note.
The review reports progress and permits an explicit retry from the same file,
including after restarting. Retrying completes the original import. It does not
overwrite later text or recreate attachments from a completed import.

One bounded private review is retained per installation. Choosing another file
replaces that review; Discard backup review removes it and preserves any notes
already restored. Filesystem permissions, owner checks and atomic file replacement
protect the local review. Gzip expansion, checksums, duplicate record identifiers,
unknown JSON fields, trailing streams, filenames and payload limits are checked.
No archive paths are extracted onto the filesystem.

## Search and organization

Search result snippets center on the first matching passage, with Unicode-safe
boundaries. Best match ranks exact titles, title prefixes, other title matches,
then body matches. Ranking operates within bounded groups of 100 matches in
repository recency order; Load more continues through every group. Recent and
Title sorts retain their original repository ordering. Search, view and tag
filters remain combined and owner-scoped.

The shared row model carries optional Unicode-code-point highlight ranges.
The WebView renders them as escaped `<mark>` elements on initial pages and live
refreshes. Existing native renderers continue to display plain titles and the
new matching snippets. Styled native highlighting is not claimed by this work.

Bulk tag, archive, unarchive, Trash, restore and undo already existed. The browser
now adds Select loaded and Clear selection, preserves the 20-note selection
limit, and keeps selection reconciled after refresh. Confirmation messages name
the selection count and consequences. A partial write reports its completed
count and does not give success haptics. Undo only covers confirmed writes and
preserves later note text. Empty states explain the current filters, and list
footers distinguish displayed rows from a total database count.
Selection mode hides the floating New note button. Browser row and header menus
are positioned synchronously and bounded to the viewport before the next tap.

## Editor conflicts and verification

Notes compares a saved editor's immutable form baseline with current persisted
text and metadata before saving. A mismatch leaves the draft open. Export or
Save text as new note preserves those edits without replacing the original;
attachments stay with the original note. The check and write serialize through
the installation's shared Notes service. Independent processes or simultaneous
independent service instances still need repository-level concurrency control.

The runtime supplies the baseline internally, so a client cannot replace it by
posting form values. A recovered draft retains its original baseline across
subsequent checkpoints and restarts. Successful save adopts the canonical
baseline; discard adopts the current saved note. Existing uncertain-save rules
still block replay.

Regression coverage includes archive round trips with exact attachment bytes,
owner-scoped review, malformed and oversized bundles, storage-full interruption,
restart/resume, concurrent restore, duplicate receipts, preservation of later
edits, partial bulk failure/undo, Unicode snippets, relevance cursor groups,
concurrent editor conflicts, and repeated draft recovery. A Chromium integration
test uses the real browser picker/download adapters, reviewed restore, live
highlight rendering and selection controls. These are Go and browser checks,
not new native-device acceptance.
