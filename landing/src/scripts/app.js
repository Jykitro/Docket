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

  /* ---------- Theme: the system one unless the visitor picked one (stored, applied in Base.astro) ---------- */
  var themeBtn = document.querySelector('.theme-btn');
  if (themeBtn) {
    var root = document.documentElement;
    var systemLight = window.matchMedia('(prefers-color-scheme: light)');
    var theme = function () { return root.dataset.theme || (systemLight.matches ? 'light' : 'dark'); };
    var syncTheme = function () {
      var light = theme() === 'light';
      var label = light ? 'Switch to dark theme' : 'Switch to light theme';
      themeBtn.classList.toggle('is-light', light);
      themeBtn.setAttribute('aria-label', label);
      themeBtn.title = label;
    };
    themeBtn.addEventListener('click', function () {
      var next = theme() === 'light' ? 'dark' : 'light';
      root.dataset.theme = next;
      try { localStorage.setItem('plandillo:theme', next); } catch (e) {}
      syncTheme();
    });
    systemLight.addEventListener('change', syncTheme);
    themeBtn.hidden = false;
    syncTheme();
  }

  /* ---------- Board demo: the planner's own board and task panel (classes follow web/src/Board.tsx, TaskPanel.tsx) ---------- */
  var BASE = [
    [{ id: 'APP-27', title: 'Add CSV export to the reports page', agent: false }, { id: 'APP-29', title: 'Draft copy for the welcome email', agent: false }],
    [{ id: 'APP-22', title: 'Move settings to the new form layout', agent: true }],
    [{ id: 'APP-18', title: 'Fix the date picker time zone offset', agent: true }, { id: 'APP-15', title: 'Update the README quick start', agent: false }]
  ];
  var FOCUS = { id: 'APP-24', title: 'Fix the mobile menu', agent: false };
  var COLUMNS = [
    { name: 'Backlog', dot: 'bg-app-bk', chip: 'bg-app-bks text-app-bkt' },
    { name: 'Doing', dot: 'bg-app-dg', chip: 'bg-app-dgs text-app-dgt' },
    { name: 'Done', dot: 'bg-app-dn', chip: 'bg-app-dns text-app-dnt' }
  ];
  var CAPTIONS = [
    'You create the task in the browser and write down how to check it.',
    'The agent picks up APP-24, moves it to Doing and notes why the menu stays open.',
    'The agent fixes the handler, tests touch and keyboard, marks it done and updates the wiki.'
  ];
  var NOTES = [
    { step: 1, time: '10:06', text: 'The link handler never closes the menu, so the overlay stays open after navigation.' },
    { step: 2, time: '10:19', text: 'Menu now closes on link click. Tested with touch and keyboard. Updated the wiki: Architecture / Navigation.' }
  ];
  var DURATIONS = [2800, 3600, 4800];
  var SPARK = 'M12 3v18M3 12h18M6 6l12 12M18 6L6 18';
  var CHECK = 'M5 12.5l4.5 4.5L19 7.5';
  var LANDED = ' animate-land motion-reduce:animate-none';

  var boardEl = document.querySelector('[data-board]');
  var statusEl = document.querySelector('[data-status]');
  var activityEl = document.querySelector('[data-activity]');
  var noteCountEl = document.querySelector('[data-note-count]');
  var openEls = document.querySelectorAll('[data-open]');
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

  function icon(d, size, sw) {
    var ns = 'http://www.w3.org/2000/svg';
    var svg = document.createElementNS(ns, 'svg');
    var attrs = { width: size, height: size, viewBox: '0 0 24 24', fill: 'none', stroke: 'currentColor', 'stroke-width': sw, 'stroke-linecap': 'round', 'stroke-linejoin': 'round', 'aria-hidden': 'true' };
    Object.keys(attrs).forEach(function (k) { svg.setAttribute(k, attrs[k]); });
    var path = document.createElementNS(ns, 'path');
    path.setAttribute('d', d);
    svg.appendChild(path);
    return svg;
  }

  // A card as the planner draws it; `fresh` is its inset ring for a task that just changed live.
  function cardNode(c, done, fresh) {
    var card = el('div', 'flex flex-col gap-2.5 rounded-[10px] border border-app-ln bg-app-sf p-3' + (fresh ? ' shadow-[inset_0_0_0_2px_var(--color-app-ac)]' + LANDED : ''));
    var top = el('div', 'flex items-center gap-2');
    if (done) {
      var ok = el('span', 'inline-flex text-app-dn');
      ok.appendChild(icon(CHECK, 15, 2.4));
      top.appendChild(ok);
    }
    top.appendChild(el('span', 'grow font-mono text-[11.5px] whitespace-nowrap text-app-t3', c.id));
    if (c.agent) {
      var badge = el('span', 'inline-flex items-center gap-1 rounded-[5px] bg-app-acs py-0.5 pr-1.5 pl-[5px] text-[11px] font-medium text-app-ac');
      badge.appendChild(icon(SPARK, 11, 2.2));
      badge.appendChild(document.createTextNode('Agent'));
      top.appendChild(badge);
    }
    top.appendChild(el('span', 'rounded-[5px] bg-app-sk px-1.5 py-0.5 font-mono text-[11px] leading-[normal] font-semibold text-app-t2', 'P1'));
    card.appendChild(top);
    card.appendChild(el('div', 'text-[14px] leading-[1.35] font-medium' + (done ? ' text-app-t2' : ''), c.title));
    return card;
  }

  var state = { step: 0, playing: !reduceMotion, prevStep: -1 };
  var timer = null;

  function render() {
    var step = state.step;
    if (boardEl) {
      boardEl.textContent = '';
      COLUMNS.forEach(function (col, i) {
        var colEl = el('div', 'flex min-w-0 flex-col gap-2.5');
        var head = el('div', 'flex h-[26px] items-center gap-2 px-1');
        head.appendChild(el('span', 'inline-block size-[9px] shrink-0 rounded-full ' + col.dot));
        head.appendChild(el('b', 'text-sm font-semibold', col.name));
        head.appendChild(el('span', 'grow font-mono text-[12px] text-app-t3 tabular-nums', String(BASE[i].length + (i === step ? 1 : 0))));
        colEl.appendChild(head);
        if (i === step) colEl.appendChild(cardNode(FOCUS, i === 2, step > 0));
        BASE[i].forEach(function (c) { colEl.appendChild(cardNode(c, i === 2, false)); });
        boardEl.appendChild(colEl);
      });
    }
    var open = String(BASE[0].length + BASE[1].length + (step < 2 ? 1 : 0));
    openEls.forEach(function (n) { n.textContent = open; });
    if (statusEl) {
      statusEl.className = 'inline-flex h-[26px] w-fit items-center gap-[7px] rounded-[7px] px-2.5 text-[13px] font-medium ' + COLUMNS[step].chip;
      statusEl.textContent = '';
      statusEl.appendChild(el('span', 'inline-block size-2 shrink-0 rounded-full ' + COLUMNS[step].dot));
      statusEl.appendChild(document.createTextNode(COLUMNS[step].name));
    }
    checks.forEach(function (c) { c.classList.toggle('is-on', step === 2); });
    if (activityEl) {
      activityEl.textContent = '';
      var shown = NOTES.filter(function (n) { return n.step <= step; });
      shown.forEach(function (n) {
        var row = el('div', 'flex gap-3' + (n.step > state.prevStep && state.prevStep >= 0 ? LANDED : ''));
        var avatar = el('span', 'grid size-7 shrink-0 place-items-center rounded-full bg-app-acs text-app-ac');
        avatar.appendChild(icon(SPARK, 14, 2));
        row.appendChild(avatar);
        var body = el('div', 'flex min-w-0 flex-1 flex-col gap-[3px]');
        var head = el('div', 'flex items-baseline gap-2');
        head.appendChild(el('b', 'text-sm font-semibold', 'Agent'));
        head.appendChild(el('span', 'font-mono text-[11.5px] text-app-t3', n.time));
        body.appendChild(head);
        body.appendChild(el('p', 'text-[13.5px] leading-[1.5]', n.text));
        row.appendChild(body);
        activityEl.appendChild(row);
      });
      if (noteCountEl) noteCountEl.textContent = String(shown.length);
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
