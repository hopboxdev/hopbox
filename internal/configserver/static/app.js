var monacoEditor = null;
var catalog = { features: [], stale: false };
var activeFeatures = {}; // ref -> true
var dirty = false;
var originalContent = '';

// ---------- init ----------

window.addEventListener('DOMContentLoaded', function () {
  loadConfig();
  loadCatalog();
  connectHeartbeat();
});

function loadConfig() {
  fetch('/config')
    .then(function (r) { return r.text(); })
    .then(function (text) {
      originalContent = text;
      populateGuided(text);
      if (monacoEditor) monacoEditor.setValue(text);
      else document.getElementById('json-fallback').value = text;
    })
    .catch(function (e) { setStatus('Failed to load config: ' + e, true); });
}

function loadCatalog() {
  fetch('/catalog')
    .then(function (r) { return r.json(); })
    .then(function (data) {
      catalog = data;
      if (data.stale) showStale();
    })
    .catch(function () {});
}

function refreshCatalog() {
  setStatus('Refreshing catalog… (this may take a minute)');
  fetch('/catalog/refresh')
    .then(function (r) { return r.json(); })
    .then(function () { pollCatalogUntilDone(0); })
    .catch(function (e) { setStatus('Catalog refresh failed: ' + e, true); });
}

function pollCatalogUntilDone(attempts) {
  if (attempts > 120) { setStatus('Catalog refresh timed out', true); return; }
  setTimeout(function () {
    fetch('/catalog')
      .then(function (r) { return r.json(); })
      .then(function (data) {
        if (data.refreshing) {
          pollCatalogUntilDone(attempts + 1);
        } else {
          catalog = data;
          setStatus('Catalog refreshed (' + (data.features || []).length + ' features)');
        }
      })
      .catch(function () { pollCatalogUntilDone(attempts + 1); });
  }, 2000);
}

function connectHeartbeat() {
  var ws = new WebSocket('ws://' + window.location.host + '/ws');
  ws.onopen = function () { document.getElementById('conn-status').textContent = '● connected'; };
  ws.onclose = function () {
    document.getElementById('conn-status').textContent = '○ disconnected';
    setTimeout(connectHeartbeat, 2000);
  };
}

// ---------- Monaco ----------

function initMonaco() {
  if (window.__monacoFailed) return;
  require(['vs/editor/editor.main'], function () {
    monacoEditor = monaco.editor.create(document.getElementById('monaco-container'), {
      value: originalContent,
      language: 'json',
      theme: 'vs-dark',
      minimap: { enabled: false },
      fontSize: 13,
      fontFamily: "ui-monospace, 'Cascadia Code', 'Fira Code', monospace",
      scrollBeyondLastLine: false,
      automaticLayout: true,
    });
    monacoEditor.onDidChangeModelContent(function () { markDirty(); });
  });
}

function initFallback() {
  document.getElementById('monaco-container').style.display = 'none';
  document.getElementById('json-fallback').style.display = 'block';
  document.getElementById('json-fallback').value = originalContent;
}

// Kick off Monaco load after the loader script fires
if (!window.__monacoFailed) {
  window.addEventListener('load', function () {
    if (typeof require !== 'undefined' && require.defined) initMonaco();
    else setTimeout(function () { if (!window.__monacoFailed) initMonaco(); }, 200);
  });
}

// ---------- mode toggle ----------

function showMode(mode) {
  document.getElementById('btn-guided').classList.toggle('active', mode === 'guided');
  document.getElementById('btn-json').classList.toggle('active', mode === 'json');
  document.getElementById('view-guided').style.display = mode === 'guided' ? '' : 'none';
  document.getElementById('view-json').style.display = mode === 'json' ? '' : 'none';

  if (mode === 'json') {
    var content = guidedToJSON();
    if (monacoEditor) monacoEditor.setValue(content);
    else document.getElementById('json-fallback').value = content;
  } else {
    var jsonContent = monacoEditor ? monacoEditor.getValue() : document.getElementById('json-fallback').value;
    try {
      populateGuided(jsonContent);
    } catch (e) {
      setStatus('Cannot switch to guided: JSON is invalid — fix in JSON mode first.', true);
      // Revert toggle
      document.getElementById('btn-guided').classList.remove('active');
      document.getElementById('btn-json').classList.add('active');
      document.getElementById('view-guided').style.display = 'none';
      document.getElementById('view-json').style.display = '';
    }
  }
}

// ---------- guided form ----------

function populateGuided(jsonText) {
  var obj = JSON.parse(jsonText);
  document.getElementById('f-name').value = obj.name || '';
  document.getElementById('f-image').value = obj.image || '';
  document.getElementById('f-user').value = obj.remoteUser || 'dev';
  document.getElementById('f-postcreate').value = obj.postCreateCommand || '';

  activeFeatures = {};
  var feats = obj.features || {};
  Object.keys(feats).forEach(function (ref) { activeFeatures[ref] = true; });
  renderChips();
}

function guidedToJSON() {
  var obj = {};
  var name = document.getElementById('f-name').value.trim();
  var image = document.getElementById('f-image').value.trim();
  var user = document.getElementById('f-user').value.trim();
  var post = document.getElementById('f-postcreate').value.trim();

  if (name) obj.name = name;
  if (image) obj.image = image;
  if (user) obj.remoteUser = user;
  if (Object.keys(activeFeatures).length > 0) {
    obj.features = {};
    Object.keys(activeFeatures).forEach(function (ref) { obj.features[ref] = {}; });
  }
  if (post) obj.postCreateCommand = post;
  return JSON.stringify(obj, null, 2);
}

function renderChips() {
  var el = document.getElementById('feature-chips');
  el.innerHTML = '';
  Object.keys(activeFeatures).forEach(function (ref) {
    var chip = document.createElement('div');
    chip.className = 'feature-chip';
    chip.innerHTML = '<span class="ref">' + ref + '</span><button class="remove" title="Remove">×</button>';
    chip.querySelector('.remove').addEventListener('click', function () {
      delete activeFeatures[ref];
      renderChips();
      markDirty();
    });
    el.appendChild(chip);
  });
}

function markDirty() { dirty = true; }

// ---------- feature search ----------

function onSearch() {
  var val = document.getElementById('feat-search').value.trim().toLowerCase();
  var dd = document.getElementById('feat-dropdown');
  dd.innerHTML = '';

  var features = (catalog.features || []);
  var results = val === ''
    ? features.slice(0, 20)
    : features.filter(function (f) {
        return f.id.toLowerCase().includes(val) || (f.name || '').toLowerCase().includes(val);
      }).slice(0, 20);

  if (results.length === 0) {
    dd.classList.remove('open');
    return;
  }

  results.forEach(function (f) {
    if (activeFeatures[f.ociRef]) return; // already added
    var row = document.createElement('div');
    row.className = 'dropdown-row';
    row.innerHTML =
      '<div>' +
        '<div class="feat-name">' + (f.name || f.id) + ' <span style="color:var(--overlay);font-size:10px">' + f.ociRef + '</span></div>' +
        '<div class="feat-desc">' + (f.description || '') + '</div>' +
      '</div>' +
      '<button class="add-btn">+</button>';
    row.querySelector('.add-btn').addEventListener('click', function (e) {
      e.stopPropagation();
      activeFeatures[f.ociRef] = true;
      renderChips();
      markDirty();
      document.getElementById('feat-search').value = '';
      dd.classList.remove('open');
    });
    dd.appendChild(row);
  });

  dd.classList.add('open');
}

document.addEventListener('click', function (e) {
  if (!e.target.closest('.search-wrap')) {
    document.getElementById('feat-dropdown').classList.remove('open');
  }
});

// ---------- save / discard ----------

function currentContent() {
  if (document.getElementById('view-json').style.display !== 'none') {
    return monacoEditor ? monacoEditor.getValue() : document.getElementById('json-fallback').value;
  }
  return guidedToJSON();
}

function saveConfig() {
  var content = currentContent();
  setStatus('Saving…');
  fetch('/save', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ content: content }),
  })
    .then(function (r) { return r.json(); })
    .then(function (data) {
      if (data.ok) {
        originalContent = content;
        dirty = false;
        setStatus('✓ saved — rebuild fires on next SSH connect');
      } else {
        setStatus('Save failed: ' + (data.errors || []).join(', '), true);
      }
    })
    .catch(function (e) { setStatus('Save error: ' + e, true); });
}

function discardChanges() {
  populateGuided(originalContent);
  if (monacoEditor) monacoEditor.setValue(originalContent);
  else document.getElementById('json-fallback').value = originalContent;
  dirty = false;
  setStatus('');
}

function setStatus(msg, isError) {
  var el = document.getElementById('status-line');
  el.textContent = msg;
  el.className = 'status-line' + (isError ? ' error' : '');
}

function showStale() {
  var banner = document.createElement('div');
  banner.className = 'stale-banner';
  banner.textContent = 'Feature catalog is stale — click ↻ to refresh';
  document.getElementById('feat-search').closest('.search-wrap').before(banner);
}
