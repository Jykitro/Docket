/* Plandillo landing — interactions. No dependencies. */
(function () {
  'use strict';

  var reduceMotion = window.matchMedia && window.matchMedia('(prefers-reduced-motion: reduce)').matches;

  /* ---------- Mobile menu ---------- */
  var menuBtn = document.querySelector('.menu-btn');
  var nav = document.getElementById('site-nav');
  if (menuBtn && nav) {
    var setMenu = function (open) {
      nav.classList.toggle('is-open', open);
      menuBtn.setAttribute('aria-expanded', String(open));
      menuBtn.setAttribute('aria-label', open ? 'Close menu' : 'Open menu');
    };
    menuBtn.addEventListener('click', function () { setMenu(!nav.classList.contains('is-open')); });
    nav.addEventListener('click', function (e) { if (e.target.closest('a')) setMenu(false); });
    document.addEventListener('keydown', function (e) { if (e.key === 'Escape') setMenu(false); });
  }

  /* ---------- Board demo ---------- */
  var BASE = [
    [{ id: 'APP-27', title: 'Add CSV export to the reports page', agent: false }, { id: 'APP-29', title: 'Draft copy for the welcome email', agent: false }],
    [{ id: 'APP-22', title: 'Move settings to the new form layout', agent: true }],
    [{ id: 'APP-18', title: 'Fix the date picker time zone offset', agent: true }, { id: 'APP-15', title: 'Update the README quick start', agent: false }]
  ];
  var COLUMNS = ['Backlog', 'Doing', 'Done'];
  var FOCUS_STATUS = ['New task, waiting for the agent', 'Agent is working on it', 'Done, checked by the agent'];
  var CAPTIONS = [
    'You create the task in the browser and write down how to check it.',
    'The agent picks up APP-24, moves it to Doing and notes why the menu stays open.',
    'The agent fixes the handler, tests touch and keyboard, marks it done and updates the wiki.'
  ];
  var ACTIVITY = [
    { agent: false, what: 'created the task', time: '10:02' },
    { agent: true, what: 'moved it to Doing', time: '10:06', note: 'The link handler never closes the menu, so the overlay stays open after navigation.' },
    { agent: true, what: 'marked it Done and updated the wiki', time: '10:19', note: 'Menu now closes on link click. Tested with touch and keyboard.' }
  ];
  var DURATIONS = [2800, 3600, 4800];

  var boardEl = document.querySelector('[data-board]');
  var statusEl = document.querySelector('[data-status]');
  var activityEl = document.querySelector('[data-activity]');
  var captionEl = document.querySelector('[data-caption]');
  var checks = document.querySelectorAll('[data-check]');
  var pills = document.querySelectorAll('.stage-pill');
  var playBtn = document.querySelector('.play-btn');

  function el(tag, cls, text) {
    var n = document.createElement(tag);
    if (cls) n.className = cls;
    if (text != null) n.textContent = text;
    return n;
  }

  function cardNode(c, focus, step) {
    var card = el('div', 'card' + (focus ? ' is-focus' : ''));
    var top = el('div', 'row-between');
    top.appendChild(el('span', 'mono dim', c.id));
    var owner = el('span', 'owner');
    owner.appendChild(el('span', 'who ' + (c.agent ? 'sq' : 'human')));
    owner.appendChild(document.createTextNode(c.agent ? 'Agent' : 'You'));
    top.appendChild(owner);
    card.appendChild(top);
    card.appendChild(el('span', 'card-title', c.title));
    if (focus) card.appendChild(el('span', 'card-status' + (step === 2 ? ' is-done' : ''), FOCUS_STATUS[step]));
    return card;
  }

  var state = { step: 0, playing: !reduceMotion, prevStep: -1 };
  var timer = null;

  function render() {
    var step = state.step;
    if (boardEl) {
      boardEl.textContent = '';
      COLUMNS.forEach(function (name, i) {
        var col = el('div', 'col');
        var head = el('div', 'col-head');
        var cards = BASE[i].slice();
        var count = cards.length + (i === step ? 1 : 0);
        head.appendChild(el('span', null, name));
        head.appendChild(el('span', null, String(count)));
        col.appendChild(head);
        if (i === step) col.appendChild(cardNode({ id: 'APP-24', title: 'Fix the mobile menu', agent: step > 0 }, true, step));
        cards.forEach(function (c) { col.appendChild(cardNode(c, false, step)); });
        boardEl.appendChild(col);
      });
    }
    if (statusEl) {
      statusEl.textContent = COLUMNS[step];
      statusEl.className = 'pill' + (step === 1 ? ' is-doing' : step === 2 ? ' is-done' : '');
    }
    checks.forEach(function (c) { c.classList.toggle('is-on', step === 2); });
    if (activityEl) {
      activityEl.textContent = '';
      for (var i = 0; i <= step; i++) {
        var a = ACTIVITY[i];
        var row = el('div', 'act' + (i > state.prevStep && state.prevStep >= 0 ? ' act-new' : ''));
        row.appendChild(el('span', 'who ' + (a.agent ? 'sq' : 'human')));
        var body = el('div', 'act-body');
        var line = el('span', 'act-line');
        line.appendChild(el('strong', null, a.agent ? 'Agent' : 'You'));
        line.appendChild(document.createTextNode(' ' + a.what + ' '));
        line.appendChild(el('span', 'dim', a.time));
        body.appendChild(line);
        if (a.note) body.appendChild(el('span', 'note', a.note));
        row.appendChild(body);
        activityEl.appendChild(row);
      }
    }
    if (captionEl) captionEl.textContent = CAPTIONS[step];
    pills.forEach(function (p) { p.setAttribute('aria-pressed', String(Number(p.dataset.stage) === step)); });
    if (playBtn) {
      playBtn.classList.toggle('is-paused', !state.playing);
      playBtn.setAttribute('aria-label', state.playing ? 'Pause the demo' : 'Play the demo');
    }
    state.prevStep = step;
  }

  function schedule() {
    clearTimeout(timer);
    if (!state.playing) return;
    timer = setTimeout(function () {
      state.step = (state.step + 1) % 3;
      if (state.step === 0) state.prevStep = -1;
      render();
      schedule();
    }, DURATIONS[state.step]);
  }

  pills.forEach(function (p) {
    p.addEventListener('click', function () {
      state.playing = false;
      clearTimeout(timer);
      state.step = Number(p.dataset.stage);
      render();
    });
  });
  if (playBtn) {
    playBtn.addEventListener('click', function () {
      state.playing = !state.playing;
      render();
      schedule();
    });
  }

  // Pause the autoplay while the demo is off screen or the tab is hidden.
  var demo = document.getElementById('workflow');
  var visible = true;
  if (demo && 'IntersectionObserver' in window) {
    new IntersectionObserver(function (entries) {
      visible = entries[0].isIntersecting;
      if (visible) schedule(); else clearTimeout(timer);
    }).observe(demo);
  }
  document.addEventListener('visibilitychange', function () {
    if (document.hidden) clearTimeout(timer); else if (visible) schedule();
  });

  if (reduceMotion) state.step = 1;
  render();
  schedule();

  /* ---------- Tabs (agent interfaces, install methods) ---------- */
  document.querySelectorAll('[data-tabs]').forEach(function (group) {
    var tabs = Array.prototype.slice.call(group.querySelectorAll('[role="tab"]'));
    function select(tab, focus) {
      tabs.forEach(function (t) {
        var on = t === tab;
        t.setAttribute('aria-selected', String(on));
        t.tabIndex = on ? 0 : -1;
        var panel = document.getElementById(t.getAttribute('aria-controls'));
        if (panel) panel.hidden = !on;
      });
      if (focus) tab.focus();
      var copy = group.querySelector('[data-copy]');
      if (copy) copy.textContent = 'Copy';
    }
    tabs.forEach(function (t, i) {
      t.addEventListener('click', function () { select(t); });
      t.addEventListener('keydown', function (e) {
        var next = null;
        if (e.key === 'ArrowRight') next = tabs[(i + 1) % tabs.length];
        if (e.key === 'ArrowLeft') next = tabs[(i - 1 + tabs.length) % tabs.length];
        if (e.key === 'Home') next = tabs[0];
        if (e.key === 'End') next = tabs[tabs.length - 1];
        if (next) { e.preventDefault(); select(next, true); }
      });
    });
  });

  /* ---------- Copy command ---------- */
  var canCopy = navigator.clipboard && navigator.clipboard.writeText;
  document.querySelectorAll('[data-copy]').forEach(function (btn) {
    var group = btn.closest('[data-tabs]');
    if (!canCopy) return;
    btn.hidden = false;
    btn.addEventListener('click', function () {
      var panel = group && group.querySelector('[role="tabpanel"]:not([hidden])');
      var text = panel ? panel.getAttribute('data-command') : '';
      if (!text) return;
      var done = function () {
        btn.textContent = 'Copied';
        setTimeout(function () { btn.textContent = 'Copy'; }, 2000);
      };
      navigator.clipboard.writeText(text).then(done, function () {
        btn.textContent = 'Select and copy';
      });
    });
  });

  /* ---------- Campaign attribution: forward utm_* to the planner, nothing else ---------- */
  var campaign = new URLSearchParams(location.search);
  document.querySelectorAll('a[data-app]').forEach(function (link) {
    var target = new URL(link.href);
    campaign.forEach(function (value, name) {
      if (/^utm_[a-z0-9_]+$/i.test(name)) target.searchParams.append(name, value);
    });
    link.href = target.href;
  });

  /* ---------- GitHub stars (public API, cached for an hour) ---------- */
  var starEl = document.querySelector('[data-github-stars]');
  if (starEl) {
    var KEY = 'plandillo:stars';
    var fmt = function (n) { return n >= 1000 ? (n / 1000).toFixed(n >= 10000 ? 0 : 1) + 'k' : String(n); };
    var show = function (n) { if (typeof n === 'number') { starEl.textContent = fmt(n); starEl.classList.remove('is-empty'); } };
    starEl.classList.add('is-empty');
    var cached = null;
    try { cached = JSON.parse(localStorage.getItem(KEY) || 'null'); } catch (e) {}
    if (cached && Date.now() - cached.t < 3600000) {
      show(cached.n);
    } else if (window.fetch) {
      fetch('https://api.github.com/repos/huntly-digital/plandillo')
        .then(function (r) { return r.ok ? r.json() : null; })
        .then(function (d) {
          if (!d || typeof d.stargazers_count !== 'number') return;
          show(d.stargazers_count);
          try { localStorage.setItem(KEY, JSON.stringify({ n: d.stargazers_count, t: Date.now() })); } catch (e) {}
        })
        .catch(function () {});
    }
  }
})();
