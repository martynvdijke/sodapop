/* Sodapop progressive enhancement: installability, offline check-in queue and
   small optional AI helpers. Core pages work without this file. */
(function () {
  'use strict';

  if ('serviceWorker' in navigator && (location.protocol === 'https:' || location.hostname === 'localhost' || location.hostname === '127.0.0.1')) {
    navigator.serviceWorker.register('/static/sw.js').catch(function () {});
  }

  var QUEUE_KEY = 'sodapop.offline.checkins.v1';

  function readQueue() {
    try { return JSON.parse(localStorage.getItem(QUEUE_KEY) || '[]'); } catch (e) { return []; }
  }
  function writeQueue(q) {
    try { localStorage.setItem(QUEUE_KEY, JSON.stringify(q)); } catch (e) {}
    renderPending();
  }
  function renderPending() {
    var n = readQueue().length;
    document.querySelectorAll('[data-pending-count]').forEach(function (el) {
      el.textContent = n ? String(n) : '';
      el.hidden = n === 0;
    });
    var status = document.getElementById('offline-status');
    if (status) status.hidden = navigator.onLine && n === 0;
  }

  function queueForm(form) {
    var data = {};
    new FormData(form).forEach(function (v, k) {
      if (typeof v === 'string') data[k] = v;
    });
    var q = readQueue();
    q.push({ data: data, at: Date.now() });
    writeQueue(q);
  }

  document.addEventListener('submit', function (event) {
    var form = event.target.closest && event.target.closest('form[data-offline-checkin]');
    if (!form || navigator.onLine) return;
    event.preventDefault();
    queueForm(form);
    var note = document.getElementById('offline-status');
    if (note) {
      note.hidden = false;
      note.textContent = 'Saved on this device. It will sync when you are back online.';
    }
    form.reset();
  });

  async function flush() {
    if (!navigator.onLine) return;
    var q = readQueue();
    if (!q.length) return;
    var remaining = [];
    for (var i = 0; i < q.length; i++) {
      try {
        var res = await fetch('/checkins', {
          method: 'POST', credentials: 'same-origin', redirect: 'follow',
          headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
          body: new URLSearchParams(q[i].data)
        });
        if (!res.ok) remaining.push(q[i]);
      } catch (e) {
        remaining.push(q[i]);
      }
    }
    writeQueue(remaining);
    if (!remaining.length) location.reload();
  }

  window.addEventListener('online', flush);
  document.addEventListener('DOMContentLoaded', function () {
    renderPending();
    flush();
  });

  // Optional AI tasting-note polish. Disabled silently when AI is not configured.
  document.addEventListener('click', async function (event) {
    var btn = event.target.closest && event.target.closest('[data-ai-polish]');
    if (!btn) return;
    var form = btn.closest('form');
    var note = form && form.querySelector('textarea[name="note"]');
    if (!note || !note.value.trim()) return;
    var csrf = form.querySelector('input[name="csrf_token"]');
    btn.disabled = true;
    var old = btn.textContent;
    btn.textContent = 'Polishing…';
    try {
      var res = await fetch('/ai/polish', {
        method: 'POST', credentials: 'same-origin',
        headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrf ? csrf.value : '' },
        body: JSON.stringify({ text: note.value })
      });
      if (res.ok) {
        var body = await res.json();
        if (body && body.note) note.value = body.note;
      }
    } catch (e) {}
    btn.disabled = false;
    btn.textContent = old;
  });
})();
