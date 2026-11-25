/* Hybrid Doctor — поведение страниц (без библиотек) */
(function () {
  'use strict';
  var $ = function (s, r) { return (r || document).querySelector(s); };
  var $$ = function (s, r) { return Array.prototype.slice.call((r || document).querySelectorAll(s)); };

  /* ---------- подтверждение опасных действий ---------- */
  document.addEventListener('click', function (e) {
    var b = e.target.closest('[data-confirm]');
    if (b && !confirm(b.getAttribute('data-confirm'))) { e.preventDefault(); e.stopPropagation(); }
  }, true);

  /* ---------- защита от двойного нажатия ---------- */
  document.addEventListener('submit', function (e) {
    var f = e.target;
    if (f.hasAttribute('data-upload')) return;
    if (f.method.toLowerCase() !== 'post') return;
    if (f.dataset.sent) { e.preventDefault(); return; }
    f.dataset.sent = '1';
    setTimeout(function () { delete f.dataset.sent; }, 4000);
  });

  /* ---------- копировать ссылку ---------- */
  $$('[data-copy]').forEach(function (b) {
    b.addEventListener('click', function () {
      var t = b.getAttribute('data-copy');
      var done = function () { var o = b.textContent; b.textContent = '✓ Скопировано'; setTimeout(function () { b.textContent = o; }, 1800); };
      if (navigator.clipboard) navigator.clipboard.writeText(t).then(done, function () { prompt('Скопируйте ссылку:', t); });
      else prompt('Скопируйте ссылку:', t);
    });
  });

  /* ---------- фото: уменьшаем на телефоне и загружаем ---------- */
  function resize(file, max, q) {
    return new Promise(function (res, rej) {
      var url = URL.createObjectURL(file), img = new Image();
      img.onload = function () {
        var k = Math.min(1, max / Math.max(img.naturalWidth, img.naturalHeight));
        var c = document.createElement('canvas');
        c.width = Math.round(img.naturalWidth * k); c.height = Math.round(img.naturalHeight * k);
        var g = c.getContext('2d'); g.fillStyle = '#fff'; g.fillRect(0, 0, c.width, c.height);
        g.drawImage(img, 0, 0, c.width, c.height);
        URL.revokeObjectURL(url);
        c.toBlob(function (b) { b ? res(b) : rej(new Error('Не удалось обработать фото')); }, 'image/jpeg', q);
      };
      img.onerror = function () { URL.revokeObjectURL(url); rej(new Error('Не удалось открыть фото')); };
      img.src = url;
    });
  }

  $$('form[data-upload]').forEach(function (f) {
    var input = $('input[type=file]', f), st = $('.upl-status', f);
    input.addEventListener('change', function () {
      var files = Array.prototype.slice.call(input.files || []);
      if (!files.length) return;
      var i = 0, ok = 0;
      function next() {
        if (i >= files.length) {
          st.textContent = 'Готово: ' + ok + ' фото';
          location.reload();
          return;
        }
        var file = files[i++];
        st.textContent = 'Загружаю ' + i + ' из ' + files.length + '…';
        Promise.all([resize(file, 1600, 0.85), resize(file, 480, 0.75)]).then(function (b) {
          var fd = new FormData(f);
          fd.delete('photo');
          fd.append('photo', b[0], 'photo.jpg');
          fd.append('thumb', b[1], 'thumb.jpg');
          return fetch(f.action, { method: 'POST', body: fd, credentials: 'same-origin', headers: { 'X-Requested-With': 'fetch' } });
        }).then(function (r) { return r.json(); }).then(function (j) {
          if (j.ok) ok++; else alert(j.message || 'Ошибка загрузки');
          next();
        }).catch(function (e) { alert((e && e.message) || 'Нет связи с сервером'); next(); });
      }
      next();
    });
  });

  /* ---------- новый заказ: поиск машины по госномеру ---------- */
  var plate = $('#plate');
  if (plate) {
    var info = $('#plateInfo'), t;
    plate.addEventListener('input', function () {
      clearTimeout(t);
      $('#car_id').value = '';
      info.hidden = true;
      var q = plate.value.trim();
      if (q.replace(/\W/g, '').length < 3) return;
      t = setTimeout(function () {
        fetch('/hybrid-doctor/api/plate?q=' + encodeURIComponent(q), { credentials: 'same-origin' }).then(function (r) { return r.json(); }).then(function (j) {
          if (!j.found) return;
          $('#car_id').value = j.car_id;
          ['last_name', 'first_name', 'phone', 'model', 'vin', 'year'].forEach(function (k) {
            var el = $('#' + k); if (el && !el.value) el.value = j[k] || '';
          });
          info.textContent = '✓ Уже был у нас: ' + j.client + ' (' + j.client_no + '), визитов: ' + j.visits + '. Данные подставлены.';
          info.hidden = false;
        }).catch(function () {});
      }, 350);
    });
  }

  /* ---------- запчасть: поиск по складу ---------- */
  $$('.pfilter').forEach(function (inp) {
    var sel = inp.parentNode.querySelector('.psel');
    var all = Array.prototype.slice.call(sel.options).map(function (o) { return o.cloneNode(true); });
    inp.addEventListener('input', function () {
      var q = inp.value.trim().toLowerCase(), first = null;
      sel.innerHTML = '';
      all.forEach(function (o) {
        if (!o.value || o.textContent.toLowerCase().indexOf(q) >= 0) {
          var c = o.cloneNode(true); sel.appendChild(c);
          if (o.value && !first) first = c;
        }
      });
      if (q && first) first.selected = true;
    });
  });

  /* ---------- установка приложения на телефон ---------- */
  if ('serviceWorker' in navigator) {
    window.addEventListener('load', function () { navigator.serviceWorker.register('/hybrid-doctor/sw.js', { scope: '/hybrid-doctor/' }).catch(function () {}); });
  }
  var standalone = window.matchMedia('(display-mode: standalone)').matches || navigator.standalone;
  var deferred = null;
  window.addEventListener('beforeinstallprompt', function (e) {
    e.preventDefault(); deferred = e;
    $$('.install-btn').forEach(function (b) { b.hidden = false; });
  });
  $$('.install-btn').forEach(function (b) {
    b.addEventListener('click', function () {
      if (!deferred) return;
      deferred.prompt();
      deferred.userChoice.finally(function () { deferred = null; b.hidden = true; });
    });
  });
  window.addEventListener('appinstalled', function () { $$('.install-btn').forEach(function (b) { b.hidden = true; }); });
  var ios = /iphone|ipad|ipod/i.test(navigator.userAgent) || (navigator.platform === 'MacIntel' && navigator.maxTouchPoints > 1);
  if (ios && !standalone) $$('.ios-hint').forEach(function (p) { p.hidden = false; });

  /* ---------- уведомления: проверяем новые назначения раз в минуту ---------- */
  if ($('.tabbar')) {
    setInterval(function () {
      if (document.hidden) return;
      fetch('/hybrid-doctor/api/pending', { credentials: 'same-origin' }).then(function (r) { return r.json(); }).then(function (j) {
        var a = $('.tabbar a[href="/hybrid-doctor/"]');
        var b = $('.badge', a);
        if (j.count > 0) {
          if (!b) { b = document.createElement('i'); b.className = 'badge'; a.insertBefore(b, a.querySelector('span')); }
          if (b.textContent !== String(j.count) && navigator.vibrate) navigator.vibrate(200);
          b.textContent = j.count;
          if (navigator.setAppBadge) navigator.setAppBadge(j.count).catch(function () {});
        } else {
          if (b) b.remove();
          if (navigator.clearAppBadge) navigator.clearAppBadge().catch(function () {});
        }
      }).catch(function () {});
    }, 60000);
  }
})();
