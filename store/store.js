// The store page: lists the registry's public apps (GET <api>/store/apps),
// offers each app's installer for the visitor's own system first, and fills
// the page around them (latest release, install steps, links) from config.js.

(() => {
  const config = window.STORE_CONFIG || {};
  // An account's own page, /@<account>/, lists that account's apps. The root
  // lists config.owner's, or the server's STORE_OWNER's.
  const account = (location.pathname.match(/^\/@([a-z0-9-]+)\/?$/) || [])[1] || '';
  // What the page shows: config.js, and on an account's page what the account adds.
  let site = config;

  // Toolchains name the same system differently (Go: windows/amd64,
  // Electron: win32/x64); the page compares and labels them as one.
  const PLATFORM_ALIASES = { win32: 'windows', macos: 'darwin', osx: 'darwin', mac: 'darwin' };
  const ARCH_ALIASES = { x64: 'amd64', x86_64: 'amd64', aarch64: 'arm64', ia32: '386', i686: '386', x86: '386' };
  const PLATFORMS = { darwin: 'macOS', windows: 'Windows', linux: 'Linux' };
  const PLATFORM_ORDER = ['darwin', 'windows', 'linux'];
  const FORMATS = {
    dmg: 'DMG',
    pkg: 'PKG',
    zip: 'ZIP',
    exe: 'EXE',
    msi: 'MSI',
    msix: 'MSIX',
    appimage: 'AppImage',
    deb: 'DEB',
    rpm: 'RPM',
    gz: 'tar.gz',
    tgz: 'tar.gz',
  };
  // Installer formats people expect first on each system.
  const PACKAGE_ORDER = {
    darwin: ['dmg', 'pkg', 'zip'],
    windows: ['exe', 'msi', 'msix', 'zip'],
    linux: ['appimage', 'deb', 'rpm', 'gz', 'zip'],
  };
  // Update metadata an older server may still list beside the installers.
  const METADATA = new Set(['blockmap', 'yml', 'yaml', 'sig']);
  const NEW_FOR_DAYS = 14;

  const STRINGS = {
    en: {
      skip: 'Skip to the apps',
      sections: 'Sections',
      navApps: 'Apps',
      navInstall: 'Install',
      navVerify: 'Checks & updates',
      title: 'Download our apps',
      seeApps: 'Browse the apps',
      howInstall: 'How to install',
      systemNote: 'You’re on {system}. Its downloads come first.',
      statApps: 'Apps',
      statSystems: 'Systems',
      statUpdated: 'Last release',
      latest: 'Latest release',
      latestEmpty: 'Nothing has been released yet. New apps will show up here.',
      latestFailed: 'The latest release couldn’t be loaded.',
      factSystem: 'System',
      factFormat: 'Format',
      factSize: 'Size',
      perksTitle: 'Why download here',
      perk1Title: 'Straight from the source',
      perk1: 'Every file downloads straight from this store, with no mirrors or ad pages in between.',
      perk2Title: 'Always the latest',
      perk2: 'Each app is listed with its newest stable release only.',
      perk3Title: 'Updates built in',
      perk3: 'Apps that support it check this store and tell you when a new version is out.',
      perk4Title: 'Easy to check',
      perk4: 'Every file lists its SHA-256, so you can confirm the download is intact.',
      appsKicker: 'Catalog',
      appsTitle: 'All apps',
      appCount: { one: '{count} app, newest stable release of each.', other: '{count} apps, newest stable release of each.' },
      appShown: 'Showing {shown} of {total} apps.',
      searchLabel: 'Search apps',
      searchPlaceholder: 'Search apps…',
      filterLabel: 'System',
      filterAll: 'All',
      loading: 'Loading apps…',
      empty: 'No apps have been published yet.',
      noStore: 'There is no store at this address.',
      accountTagline: 'Apps published by {account}.',
      failed: 'The apps couldn’t be loaded. Check your connection, then try again.',
      noMatch: 'No app matches. Try another name or system.',
      retry: 'Try again',
      download: 'Download for {system}',
      downloadPlain: 'Download',
      version: 'Version {version}',
      updated: 'updated {when}',
      justNow: 'just now',
      new: 'New',
      whatsNew: 'What’s new in {version}',
      pricingTitle: 'Plans and prices',
      perServerYear: '/ server / year',
      perYear: '/ year',
      free: 'Free',
      fromPrice: 'from {price}',
      buy: 'Buy',
      contactSales: 'Contact us',
      eula: 'EULA',
      files: { one: 'All downloads ({count} file)', other: 'All downloads ({count} files)' },
      copy: 'Copy the SHA-256 of {file}',
      copied: 'SHA-256 copied',
      homepage: 'About this app',
      noBuild: 'No {system} version yet; other systems are below.',
      other: 'Other',
      channels: { stable: 'Stable', beta: 'Beta' },
      archs: { arm64: 'ARM64', amd64: 'x64', universal: 'Universal', 386: 'x86' },
      macArchs: { arm64: 'Apple silicon', amd64: 'Intel' },
      installKicker: 'Guide',
      installTitle: 'Installing an app',
      installLede: 'Download the file for your system, then follow its steps. Your system is marked.',
      yours: 'Your system',
      mac1: 'Download the .dmg, or the .zip.',
      mac2: 'Open it and drag the app into Applications.',
      mac3: 'If macOS won’t open it the first time, go to System Settings → Privacy & Security and choose Open Anyway.',
      win1: 'Download the setup .exe, or the .zip.',
      win2: 'Run it and follow the installer.',
      win3: 'If SmartScreen says “Windows protected your PC”, choose More info → Run anyway.',
      lin1: 'AppImage: make it executable, then run it.',
      lin2: '.deb: install it with apt.',
      lin3: '.zip or .tar.gz: unpack it and run the program inside.',
      verifyKicker: 'Check',
      verifyTitle: 'Check what you downloaded',
      verifyLede: 'Compare the file’s SHA-256 with the one under All downloads. They match only if the file arrived intact.',
      updatesKicker: 'Updates',
      updatesTitle: 'Staying up to date',
      updatesText:
        'Apps that support it ask this store for new versions, then offer to install them. You can always come back here for the latest installer.',
      helpTitle: 'Need a hand?',
      helpText: 'Missing a version for your system, or stuck installing? Write to us.',
      footStore: 'Store',
      footContact: 'Contact',
      rights: '© {year} {company}',
      powered: 'Served by',
    },
    vi: {
      skip: 'Bỏ qua, tới danh sách ứng dụng',
      sections: 'Các mục',
      navApps: 'Ứng dụng',
      navInstall: 'Cài đặt',
      navVerify: 'Kiểm tra và cập nhật',
      title: 'Tải ứng dụng',
      seeApps: 'Xem các ứng dụng',
      howInstall: 'Cách cài đặt',
      systemNote: 'Bạn đang dùng {system}. Bản tải cho máy này được ưu tiên.',
      statApps: 'Ứng dụng',
      statSystems: 'Hệ điều hành',
      statUpdated: 'Phát hành gần nhất',
      latest: 'Bản mới nhất',
      latestEmpty: 'Chưa có bản phát hành nào. Ứng dụng mới sẽ xuất hiện ở đây.',
      latestFailed: 'Không tải được bản mới nhất.',
      factSystem: 'Hệ điều hành',
      factFormat: 'Định dạng',
      factSize: 'Dung lượng',
      perksTitle: 'Vì sao tải ở đây',
      perk1Title: 'Tải trực tiếp',
      perk1: 'Mỗi file được tải thẳng từ store này, không qua trang trung gian hay quảng cáo.',
      perk2Title: 'Luôn là bản mới nhất',
      perk2: 'Mỗi ứng dụng chỉ hiện bản ổn định mới nhất.',
      perk3Title: 'Tự báo cập nhật',
      perk3: 'Ứng dụng có hỗ trợ sẽ tự kiểm tra store này và báo khi có bản mới.',
      perk4Title: 'Dễ kiểm tra',
      perk4: 'Mỗi file kèm mã SHA-256 để bạn chắc file tải về còn nguyên vẹn.',
      appsKicker: 'Danh mục',
      appsTitle: 'Tất cả ứng dụng',
      appCount: { other: '{count} ứng dụng, mỗi ứng dụng là bản ổn định mới nhất.' },
      appShown: 'Đang hiện {shown} trên {total} ứng dụng.',
      searchLabel: 'Tìm ứng dụng',
      searchPlaceholder: 'Tìm ứng dụng…',
      filterLabel: 'Hệ điều hành',
      filterAll: 'Tất cả',
      loading: 'Đang tải danh sách ứng dụng…',
      empty: 'Chưa có ứng dụng nào được phát hành.',
      noStore: 'Không có store nào ở địa chỉ này.',
      accountTagline: 'Ứng dụng do {account} phát hành.',
      failed: 'Không tải được danh sách ứng dụng. Hãy kiểm tra kết nối rồi thử lại.',
      noMatch: 'Không có ứng dụng nào khớp. Thử tên hoặc hệ điều hành khác.',
      retry: 'Thử lại',
      download: 'Tải cho {system}',
      downloadPlain: 'Tải về',
      version: 'Phiên bản {version}',
      updated: 'cập nhật {when}',
      justNow: 'vừa xong',
      new: 'Mới',
      whatsNew: 'Có gì mới trong {version}',
      pricingTitle: 'Gói và giá',
      perServerYear: '/ máy chủ / năm',
      perYear: '/ năm',
      free: 'Miễn phí',
      fromPrice: 'từ {price}',
      buy: 'Mua',
      contactSales: 'Liên hệ',
      eula: 'EULA',
      files: { other: 'Tất cả bản tải ({count} file)' },
      copy: 'Chép mã SHA-256 của {file}',
      copied: 'Đã chép mã SHA-256',
      homepage: 'Giới thiệu sản phẩm',
      noBuild: 'Chưa có bản cho {system}; các bản khác ở bên dưới.',
      other: 'Khác',
      channels: { stable: 'Ổn định', beta: 'Beta' },
      archs: { arm64: 'ARM64', amd64: 'x64', universal: 'Universal', 386: 'x86' },
      macArchs: { arm64: 'Apple silicon', amd64: 'Intel' },
      installKicker: 'Hướng dẫn',
      installTitle: 'Cài đặt ứng dụng',
      installLede: 'Tải file cho hệ điều hành của bạn, rồi làm theo các bước tương ứng. Hệ điều hành của bạn được đánh dấu.',
      yours: 'Máy của bạn',
      mac1: 'Tải file .dmg (hoặc .zip).',
      mac2: 'Mở file và kéo ứng dụng vào thư mục Applications.',
      mac3: 'Nếu macOS không cho mở lần đầu: vào System Settings → Privacy & Security, chọn Open Anyway.',
      win1: 'Tải file cài đặt .exe (hoặc .zip).',
      win2: 'Chạy file và làm theo trình cài đặt.',
      win3: 'Nếu SmartScreen báo “Windows protected your PC”: chọn More info → Run anyway.',
      lin1: 'AppImage: cấp quyền chạy rồi mở file.',
      lin2: '.deb: cài bằng apt.',
      lin3: '.zip hoặc .tar.gz: giải nén rồi chạy chương trình bên trong.',
      verifyKicker: 'Kiểm tra',
      verifyTitle: 'Kiểm tra file đã tải',
      verifyLede: 'So mã SHA-256 của file với mã trong mục Tất cả bản tải. Hai mã chỉ khớp khi file tải về còn nguyên vẹn.',
      updatesKicker: 'Cập nhật',
      updatesTitle: 'Luôn dùng bản mới',
      updatesText:
        'Ứng dụng có hỗ trợ sẽ hỏi store này khi có phiên bản mới và đề nghị cài đặt. Bạn cũng có thể quay lại đây để tải bộ cài mới nhất.',
      helpTitle: 'Cần hỗ trợ?',
      helpText: 'Chưa có bản cho máy của bạn, hoặc gặp lỗi khi cài? Hãy viết cho chúng tôi.',
      footStore: 'Store',
      footContact: 'Liên hệ',
      rights: '© {year} {company}',
      powered: 'Vận hành bởi',
    },
  };

  const LANG_KEY = 'store.lang';
  const saved = {
    get: (key) => {
      try {
        return localStorage.getItem(key);
      } catch {
        return null;
      }
    },
    set: (key, value) => {
      try {
        localStorage.setItem(key, value);
      } catch {
        // Private windows: the choice just isn't remembered.
      }
    },
  };

  const $ = (selector) => document.querySelector(selector);
  const $$ = (selector) => document.querySelectorAll(selector);
  const fill = (text, values) => text.replace(/\{(\w+)\}/g, (_, key) => values[key] ?? '');

  // ?lang=vi|en opens the page in that language, for links from a site that
  // knows which one its reader uses. It holds for this visit only: what the
  // page remembers is the visitor's own pick on the switcher.
  const asked = new URLSearchParams(location.search).get('lang');
  let lang = Object.keys(STRINGS).includes(asked)
    ? asked
    : saved.get(LANG_KEY) || config.defaultLang || (navigator.language || '').slice(0, 2);
  if (!STRINGS[lang]) lang = 'en';
  let apps = null; // null until loaded, or after loading failed
  let failed = false;
  let missing = false; // an account page for an account that doesn't exist
  // What the registry sells (GET /store/products): prices come from the server.
  let products = [];
  let ordersEnabled = false;
  let storeContact = '';
  const system = { platform: detectPlatform(), arch: null };
  const filter = { platform: '', query: '' };

  const t = () => STRINGS[lang];
  const locale = () => (lang === 'vi' ? 'vi-VN' : 'en-GB');
  // A config value given per language ({ vi, en }) or once for both.
  const local = (value) => (value && typeof value === 'object' ? (value[lang] ?? value.en ?? '') : value) || '';
  const plural = (forms, count) => fill(forms[new Intl.PluralRules(locale()).select(count)] || forms.other, { count });

  function detectPlatform() {
    if (/android|iphone|ipod/i.test(navigator.userAgent)) return null;
    const name = (navigator.userAgentData?.platform || navigator.platform || navigator.userAgent || '').toLowerCase();
    if (name.includes('mac')) return 'darwin';
    if (name.includes('win')) return 'windows';
    if (name.includes('linux') || name.includes('x11')) return 'linux';
    return null;
  }

  async function detectArch() {
    try {
      const values = await navigator.userAgentData?.getHighEntropyValues(['architecture']);
      if (values?.architecture === 'arm') return 'arm64';
      if (values?.architecture === 'x86') return 'amd64';
    } catch {
      // Not offered by this browser.
    }
    return null;
  }

  // Building blocks

  function el(tag, className, text) {
    const node = document.createElement(tag);
    if (className) node.className = className;
    if (text != null) node.textContent = text;
    return node;
  }

  function icon(name, className = 'icon') {
    const svg = document.createElementNS('http://www.w3.org/2000/svg', 'svg');
    svg.setAttribute('class', className);
    svg.setAttribute('aria-hidden', 'true');
    const use = document.createElementNS('http://www.w3.org/2000/svg', 'use');
    use.setAttribute('href', `#i-${name}`);
    svg.append(use);
    return svg;
  }

  const osIcon = (os) => (PLATFORMS[os] ? os : 'other');
  const platformName = (os) => PLATFORMS[os] || os || t().other;
  const formatName = (d) => FORMATS[d.pkg] || d.pkg.toUpperCase();

  function archName(d) {
    if (!d.cpu) return '';
    return (d.os === 'darwin' && t().macArchs[d.cpu]) || t().archs[d.cpu] || d.arch || d.cpu;
  }

  const systemName = (d) => [platformName(d.os), archName(d)].filter(Boolean).join(' · ');

  function size(bytes) {
    if (!bytes) return '';
    const units = ['B', 'KB', 'MB', 'GB'];
    let value = bytes;
    let unit = 0;
    while (value >= 1024 && unit < units.length - 1) {
      value /= 1024;
      unit += 1;
    }
    const digits = unit > 0 && value < 10 ? 1 : 0;
    return `${new Intl.NumberFormat(locale(), { maximumFractionDigits: digits }).format(value)} ${units[unit]}`;
  }

  const vnd = (n) => new Intl.NumberFormat('vi-VN', { style: 'currency', currency: 'VND', maximumFractionDigits: 0 }).format(n);

  // The product sold under an app, matched by the app's registry name.
  function pricingFor(item) {
    const name = String(item.app.name || '');
    const key = name.toLowerCase().replace(/[^a-z0-9]+/g, '-').replace(/^-|-$/g, '');
    return products.find((p) => p.app === name || p.id === key) || null;
  }

  function buyable(product) {
    return (ordersEnabled && product?.plans.find((plan) => plan.purchasable)) || null;
  }

  function renderPricing(node, item) {
    const s = t();
    const product = pricingFor(item);
    const section = node.querySelector('.pricing');
    if (!product || account) return; // an account's own store sells nothing here
    section.hidden = false;
    section.querySelector('.pricing__title').textContent = s.pricingTitle;
    const features = extra(item.app).pricing?.features || {};
    const list = section.querySelector('.plans');
    for (const plan of product.plans) {
      const row = $('#plan-row').content.firstElementChild.cloneNode(true);
      row.classList.toggle('plan--highlight', Boolean(plan.purchasable));
      row.querySelector('.plan__name').textContent = plan.name;
      const amount = row.querySelector('.plan__amount');
      const per = row.querySelector('.plan__per');
      if (plan.contact) {
        amount.textContent = fill(s.fromPrice, { price: vnd(plan.from_vnd || 0) });
        per.textContent = s.perYear;
      } else if (plan.vnd) {
        amount.textContent = vnd(plan.vnd);
        per.textContent = plan.per === 'cluster' ? s.perServerYear : s.perYear;
      } else {
        amount.textContent = s.free;
      }
      const bullets = row.querySelector('.plan__features');
      for (const text of features[plan.id]?.[lang] || []) bullets.append(el('li', null, text));
      const action = row.querySelector('.plan__action');
      const contact = storeContact || site.contact || '';
      if (plan.purchasable && ordersEnabled && window.StoreBuy) {
        action.textContent = s.buy;
        action.href = '#';
        action.classList.replace('btn--quiet', 'btn--primary');
        action.addEventListener('click', (event) => {
          event.preventDefault();
          window.StoreBuy.open({ product, plan, title: item.title });
        });
      } else if ((plan.contact || plan.purchasable) && contact) {
        action.textContent = s.contactSales;
        action.href = `mailto:${contact}?subject=${encodeURIComponent(`${item.title} ${plan.name}`)}`;
      } else {
        action.textContent = s.downloadPlain;
        action.href = pick(item.app.downloads).url;
      }
      list.append(row);
    }
  }

  const shortHash = (hash) => (hash.length > 20 ? `${hash.slice(0, 8)}…${hash.slice(-8)}` : hash);
  const day = (date) => new Intl.DateTimeFormat(locale(), { dateStyle: 'medium' }).format(date);

  function ago(date) {
    const seconds = (date.getTime() - Date.now()) / 1000;
    const units = [
      ['year', 31536000],
      ['month', 2592000],
      ['week', 604800],
      ['day', 86400],
      ['hour', 3600],
      ['minute', 60],
    ];
    const format = new Intl.RelativeTimeFormat(locale(), { numeric: 'auto' });
    for (const [unit, length] of units) {
      if (Math.abs(seconds) >= length) return format.format(Math.round(seconds / length), unit);
    }
    return t().justNow;
  }

  // Apps

  function normalize(app) {
    const downloads = (app.downloads || [])
      .map((d) => ({
        ...d,
        os: PLATFORM_ALIASES[d.platform] || d.platform || '',
        cpu: ARCH_ALIASES[d.arch] || d.arch || '',
        pkg: String(d.package || '').toLowerCase(),
      }))
      .filter((d) => d.url && !METADATA.has(d.pkg));
    return { ...app, downloads };
  }

  // What config.js adds to an app: a display name, texts, a homepage. Matched
  // by the registry name as is, or lowercased with dashes ("LLM Proxy" → llm-proxy).
  function extra(app) {
    const apps = site.apps || {};
    const key = String(app.name || '')
      .toLowerCase()
      .replace(/[^a-z0-9]+/g, '-')
      .replace(/^-|-$/g, '');
    return apps[app.name] || apps[key] || {};
  }

  function view(app) {
    const more = extra(app);
    return {
      app,
      title: local(more.title) || app.name,
      tagline: local(more.tagline),
      description: local(more.description) || app.description || '',
      homepage: local(more.homepage),
      logo: local(more.logo) || app.logo || '',
      updated: app.updated_at ? new Date(app.updated_at) : null,
    };
  }

  const packageRank = (d) => {
    const order = PACKAGE_ORDER[d.os] || [];
    const index = order.indexOf(d.pkg);
    return index === -1 ? order.length : index;
  };

  const platformRank = (d) => {
    if (d.os === system.platform) return -1;
    const index = PLATFORM_ORDER.indexOf(d.os);
    return index === -1 ? PLATFORM_ORDER.length : index;
  };

  // This machine's architecture (or a universal build) before the others.
  const archRank = (d) => (system.arch && d.cpu === system.arch ? 0 : d.cpu === 'universal' ? 1 : 2);

  const sorted = (downloads) =>
    [...downloads].sort(
      (a, b) =>
        platformRank(a) - platformRank(b) || archRank(a) - archRank(b) || a.cpu.localeCompare(b.cpu) || packageRank(a) - packageRank(b),
    );

  // The installer to offer first: this system, this architecture (or a
  // universal build), in the format people expect here.
  function bestDownload(downloads) {
    const mine = downloads.filter((d) => d.os === system.platform);
    if (!mine.length) return null;
    const archRank = (d) => {
      if (system.arch && d.cpu === system.arch) return 0;
      if (d.cpu === 'universal') return 1;
      if (!system.arch && system.platform === 'darwin' && d.cpu === 'arm64') return 2;
      if (!system.arch && system.platform !== 'darwin' && d.cpu === 'amd64') return 2;
      return 3;
    };
    return [...mine].sort((a, b) => archRank(a) - archRank(b) || packageRank(a) - packageRank(b))[0];
  }

  const pick = (downloads) => bestDownload(downloads) || sorted(downloads)[0];
  const downloadLabel = (d) => (d.os ? fill(t().download, { system: platformName(d.os) }) : t().downloadPlain);

  function paintLogo(node, item) {
    node.textContent = '';
    node.classList.toggle('has-img', /^https?:\/\//i.test(item.logo));
    if (node.classList.contains('has-img')) {
      const img = el('img');
      img.src = item.logo;
      img.alt = '';
      img.loading = 'lazy';
      img.decoding = 'async';
      node.append(img);
      return;
    }
    let hue = 0;
    for (const char of item.title) hue = (hue * 31 + char.codePointAt(0)) % 360;
    node.style.setProperty('--hue', String(hue));
    node.textContent = item.title.trim().charAt(0).toUpperCase() || '?';
  }

  const isNew = (item) => item.updated && Date.now() - item.updated.getTime() < NEW_FOR_DAYS * 86400000;

  function card(item) {
    const s = t();
    const { app } = item;
    const node = $('#card-template').content.firstElementChild.cloneNode(true);
    // What a link to one app, #app-<name>, opens the page at.
    node.id = `app-${app.name}`;

    paintLogo(node.querySelector('.app__logo'), item);
    node.querySelector('.app__name').textContent = item.title;
    const meta = node.querySelector('.app__meta');
    meta.textContent = [fill(s.version, { version: app.version }), item.updated ? fill(s.updated, { when: ago(item.updated) }) : '']
      .filter(Boolean)
      .join(' · ');
    if (item.updated) meta.title = day(item.updated);
    const badge = node.querySelector('.app__new');
    badge.hidden = !isNew(item);
    badge.textContent = s.new;

    const description = node.querySelector('.app__description');
    description.textContent = item.description;
    description.hidden = !item.description;

    const systems = node.querySelector('.app__systems');
    const platforms = [...new Set(sorted(app.downloads).map((d) => d.os))];
    for (const os of platforms) {
      const archs = [...new Set(app.downloads.filter((d) => d.os === os).map(archName).filter(Boolean))];
      const chip = el('li', 'chip');
      chip.append(icon(osIcon(os)), el('strong', null, platformName(os)));
      if (archs.length) chip.append(el('span', null, archs.join(', ')));
      systems.append(chip);
    }

    renderPricing(node, item);

    if (app.changes) {
      const changes = node.querySelector('.app__changes');
      changes.hidden = false;
      changes.querySelector('summary').textContent = fill(s.whatsNew, { version: app.version });
      changes.querySelector('p').textContent = app.changes;
    }

    if (item.homepage) {
      const homepage = node.querySelector('.app__homepage');
      homepage.hidden = false;
      homepage.href = item.homepage;
      homepage.querySelector('span').textContent = s.homepage;
    }

    const best = bestDownload(app.downloads);
    const first = best || sorted(app.downloads)[0];
    const primary = node.querySelector('.app__primary');
    primary.href = first.url;
    node.querySelector('.app__primary-label').textContent = downloadLabel(first);
    node.querySelector('.app__primary-detail').textContent = [archName(first), formatName(first), size(first.size)]
      .filter(Boolean)
      .join(' · ');
    const note = node.querySelector('.app__note');
    note.hidden = Boolean(best) || !system.platform;
    note.textContent = fill(s.noBuild, { system: platformName(system.platform) });

    node.querySelector('.app__files summary').textContent = plural(s.files, app.downloads.length);
    const files = node.querySelector('.files');
    for (const d of sorted(app.downloads)) {
      const row = $('#file-row').content.firstElementChild.cloneNode(true);
      row.querySelector('use').setAttribute('href', `#i-${osIcon(d.os)}`);
      const link = row.querySelector('.file__name');
      link.href = d.url;
      link.textContent = systemName(d);
      row.querySelector('.file__meta').textContent = [formatName(d), size(d.size)].filter(Boolean).join(' · ');
      if (d.sha256) {
        const hash = row.querySelector('.file__hash');
        hash.hidden = false;
        hash.dataset.hash = d.sha256;
        hash.title = `SHA-256 ${d.sha256}`;
        hash.setAttribute('aria-label', fill(s.copy, { file: `${systemName(d)} ${formatName(d)}` }));
        hash.querySelector('code').textContent = `${d.sha256.slice(0, 8)}…`; // the button copies all of it
      }
      files.append(row);
    }
    return node;
  }

  // Page parts

  function renderTicket(items) {
    const s = t();
    const body = $('[data-ticket]');
    const date = $('[data-ticket-date]');
    if (!apps && !failed) return; // still loading: the skeleton stays
    body.textContent = '';
    date.textContent = '';
    if (failed) {
      body.append(el('p', 'ticket__empty', s.latestFailed));
      return;
    }
    const latest = [...items].sort((a, b) => (b.updated?.getTime() || 0) - (a.updated?.getTime() || 0))[0];
    if (!latest) {
      body.append(el('p', 'ticket__empty', missing ? s.noStore : s.latestEmpty));
      return;
    }
    if (latest.updated) date.textContent = day(latest.updated);

    const head = el('div', 'ticket__app');
    const logo = el('span', 'logo');
    logo.setAttribute('aria-hidden', 'true');
    paintLogo(logo, latest);
    const names = el('div');
    names.append(el('p', 'ticket__name', latest.title), el('p', 'ticket__version', fill(s.version, { version: latest.app.version })));
    head.append(logo, names, el('span', 'stamp', s.channels[latest.app.channel] || latest.app.channel));
    body.append(head);

    const blurb = latest.tagline || latest.description;
    if (blurb) body.append(el('p', 'ticket__tagline', blurb));

    const d = pick(latest.app.downloads);
    const facts = el('dl', 'ticket__facts');
    for (const [term, value] of [
      [s.factSystem, systemName(d)],
      [s.factFormat, formatName(d)],
      [s.factSize, size(d.size) || '—'],
    ]) {
      const fact = el('div');
      fact.append(el('dt', null, term), el('dd', null, value));
      fact.lastChild.title = value;
      facts.append(fact);
    }
    body.append(facts);

    const button = el('a', 'btn btn--primary btn--block');
    button.href = d.url;
    button.append(icon('download'), el('span', null, downloadLabel(d)));
    body.append(button);

    const plan = account ? null : buyable(pricingFor(latest));
    if (plan && window.StoreBuy) {
      const buy = el('a', 'ticket__buy');
      buy.href = '#';
      buy.append(icon('tag'), el('span', null, `${s.buy} ${plan.name} · ${vnd(plan.vnd)} ${s.perServerYear}`));
      buy.addEventListener('click', (event) => {
        event.preventDefault();
        window.StoreBuy.open({ product: pricingFor(latest), plan, title: latest.title });
      });
      body.append(buy);
    }

    if (d.sha256) {
      const hash = el('p', 'ticket__hash');
      const code = el('code', null, shortHash(d.sha256));
      code.title = d.sha256;
      hash.append(icon('shield'), el('span', null, 'SHA-256'), code);
      body.append(hash);
    }
  }

  function renderStats(items) {
    const put = (key, value, sub) => {
      const node = $(`[data-stat="${key}"]`);
      node.textContent = value;
      if (sub) node.append(el('span', 'stat__sub', sub));
    };
    if (!apps) {
      ['apps', 'systems', 'updated'].forEach((key) => put(key, '–'));
      return;
    }
    put('apps', String(items.length));
    const known = PLATFORM_ORDER.filter((os) => items.some((item) => item.app.downloads.some((d) => d.os === os)));
    put('systems', String(known.length), known.map(platformName).join(', '));
    const latest = items.reduce((max, item) => (item.updated && (!max || item.updated > max) ? item.updated : max), null);
    put('updated', latest ? ago(latest) : '–', latest ? day(latest) : '');
  }

  function matches(item) {
    if (filter.platform && !item.app.downloads.some((d) => d.os === filter.platform)) return false;
    const query = filter.query.trim().toLowerCase();
    if (!query) return true;
    return [item.title, item.app.name, item.tagline, item.description].join(' ').toLowerCase().includes(query);
  }

  function renderApps(items) {
    const s = t();
    const list = $('[data-apps]');
    if (!apps && !failed) return; // still loading: the skeleton stays
    list.textContent = '';
    list.setAttribute('aria-busy', 'false');

    $('[data-tools]').hidden = !apps || items.length < 2;
    $$('[data-filter]').forEach((button) => {
      const os = button.dataset.filter;
      button.setAttribute('aria-pressed', String(os === filter.platform));
      let count = button.querySelector('.count');
      if (!count) {
        count = el('span', 'count');
        button.append(count);
      }
      count.textContent = String(os ? items.filter((item) => item.app.downloads.some((d) => d.os === os)).length : items.length);
    });

    const shown = items.filter(matches);
    const filtered = Boolean(filter.platform || filter.query.trim());
    const count = $('[data-count]');
    count.hidden = !apps;
    count.textContent = filtered ? fill(s.appShown, { shown: shown.length, total: items.length }) : plural(s.appCount, items.length);

    if (failed) setState('failed');
    else if (!items.length) setState(missing ? 'noStore' : 'empty');
    else if (!shown.length) setState('noMatch');
    else setState(null);

    for (const item of shown) list.append(card(item));
  }

  function renderGuides() {
    $$('[data-guide]').forEach((guide) => {
      const yours = guide.dataset.guide === system.platform;
      guide.classList.toggle('guide--yours', yours);
      guide.querySelector('.badge').hidden = !yours;
    });
  }

  function render() {
    const items = apps ? apps.map(view) : [];
    renderStats(items);
    renderTicket(items);
    renderApps(items);
    renderGuides();
  }

  function setState(key) {
    $('[data-status]').hidden = !key;
    $('[data-state]').textContent = key ? t()[key] : '';
    $('[data-retry]').hidden = key !== 'failed';
  }

  function translate() {
    const s = t();
    document.documentElement.lang = lang;
    $$('[data-t]').forEach((node) => {
      const text = s[node.dataset.t];
      if (typeof text === 'string') node.textContent = text;
    });
    $$('[data-t-label]').forEach((node) => node.setAttribute('aria-label', s[node.dataset.tLabel]));
    $('[data-search]').placeholder = s.searchPlaceholder;
    $$('[data-lang]').forEach((button) => button.setAttribute('aria-pressed', String(button.dataset.lang === lang)));

    const tagline = local(site.tagline);
    $('[data-tagline]').textContent = tagline;
    $('meta[name="description"]').setAttribute('content', tagline || s.title);

    const note = $('[data-system]');
    note.hidden = !system.platform;
    if (system.platform) {
      $('[data-system-icon]').setAttribute('href', `#i-${system.platform}`);
      const name = systemName({ os: system.platform, cpu: system.arch || '' });
      $('[data-system-text]').textContent = fill(s.systemNote, { system: name });
    }

    const home = local(site.homeUrl);
    $$('[data-home]').forEach((link) => (home ? (link.href = home) : link.removeAttribute('href')));
    const siteLink = $('[data-site]');
    siteLink.hidden = !home;
    if (home) {
      siteLink.href = home;
      $('[data-site-label]').textContent = new URL(home, location.href).hostname.replace(/^www\./, '');
    }

    const about = $('[data-about]');
    about.textContent = local(site.about);
    about.hidden = !about.textContent;

    const links = Array.isArray(site.links) ? site.links : [];
    $('[data-links-col]').hidden = !links.length;
    $('[data-links-title]').textContent = local(site.linksTitle) || site.company || site.name || '';
    const list = $('[data-links]');
    list.textContent = '';
    for (const link of links) {
      const item = el('li');
      const anchor = el('a', null, local(link.label));
      anchor.href = local(link.url);
      item.append(anchor);
      list.append(item);
    }

    const contact = site.contact || '';
    $('[data-help]').hidden = !contact;
    $('[data-contact-col]').hidden = !contact;
    if (contact) {
      for (const node of [$('[data-contact-link]'), $('[data-contact-mail]')]) node.href = `mailto:${contact}`;
      $('[data-contact-text]').textContent = contact;
      $('[data-contact-mail]').textContent = contact;
    }

    $('[data-copyright]').textContent = fill(s.rights, {
      year: new Date().getFullYear(),
      company: site.company || site.name || '',
    });
    $('[data-credit]').hidden = site.credit === false;
  }

  async function loadProducts() {
    try {
      const response = await fetch(`${config.api || '/api'}/store/products`, { headers: { Accept: 'application/json' } });
      if (!response.ok) return;
      const data = await response.json();
      products = Array.isArray(data.products) ? data.products : [];
      ordersEnabled = Boolean(data.orders_enabled);
      storeContact = data.contact || '';
    } catch (error) {
      console.error('store: loading products failed', error);
    }
  }

  async function load() {
    // A first load shows the skeleton; a retry says it is trying again.
    setState(failed ? 'loading' : null);
    failed = false;
    const query = new URLSearchParams({ channel: config.channel || 'stable' });
    if (account || config.owner) query.set('owner', account || config.owner);
    try {
      const response = await fetch(`${config.api || '/api'}/store/apps?${query}`, { headers: { Accept: 'application/json' } });
      missing = Boolean(account) && response.status === 404;
      if (!response.ok && !missing) throw new Error(`HTTP ${response.status}`);
      const data = missing ? {} : await response.json();
      apps = (data.apps || []).map(normalize).filter((app) => app.downloads.length);
      site = siteFor(data);
      brand();
      translate();
    } catch (error) {
      console.error('store: loading apps failed', error);
      apps = null;
      failed = true;
    }
    render();
  }

  // An account's page: with a profile (plans with store branding) it is the
  // account's own store; without one it stays the shared store, naming the account.
  function siteFor(data) {
    if (!account) return config;
    const profile = data.profile;
    const shared = { ...config, apps: {} };
    if (!profile) {
      const text = missing ? 'noStore' : 'accountTagline';
      const tagline = Object.fromEntries(Object.entries(STRINGS).map(([code, s]) => [code, fill(s[text], { account })]));
      return { ...shared, eyebrow: `@${account}`, title: `${account} · ${config.name || 'App Store'}`, tagline };
    }
    const name = profile.name || account;
    return {
      ...shared,
      name,
      company: name,
      eyebrow: name,
      logo: profile.logo || '',
      homeUrl: profile.homeUrl || '',
      tagline: profile.tagline || '',
      about: profile.about || '',
      contact: profile.contact || '',
      links: profile.links || [],
      linksTitle: name,
      credit: false,
    };
  }

  function brand() {
    const name = site.name || 'App Store';
    document.title = site.title || name;
    $$('[data-name]').forEach((node) => (node.textContent = name));
    $('[data-eyebrow]').textContent = site.eyebrow || name;
    $$('[data-mark]').forEach((node) => {
      node.textContent = '';
      node.classList.toggle('has-img', Boolean(site.logo));
      if (site.logo) {
        const img = el('img');
        img.src = site.logo;
        img.alt = '';
        node.append(img);
      } else {
        node.textContent = name.charAt(0).toUpperCase();
      }
    });
    $('[data-icon]').href = site.logo || 'data:,';
  }

  // A link to #app-<name> opens the page at that app's card. The cards arrive
  // after the browser has looked for the link's target, so the page goes there
  // itself once everything that moves them (the apps, prices, this machine's
  // architecture) has rendered, unless the visitor has scrolled meanwhile. It
  // jumps there, where store.css's smooth scrolling would glide past every
  // card above it. The browser mustn't restore an earlier scroll position
  // first, as on a reload: it does so before the cards exist, and the page
  // shifts under it when they arrive.
  if (location.hash.startsWith('#app-') && 'scrollRestoration' in history) history.scrollRestoration = 'manual';
  function showLinkedApp() {
    let id = '';
    try {
      id = decodeURIComponent(location.hash.slice(1));
    } catch {
      return; // a malformed link
    }
    const target = id.startsWith('app-') && document.getElementById(id);
    if (!target || window.scrollY > 0) return;
    const root = document.documentElement;
    root.style.scrollBehavior = 'auto';
    target.scrollIntoView();
    root.style.scrollBehavior = '';
  }

  function setup() {
    brand();
    $$('[data-lang]').forEach((button) =>
      button.addEventListener('click', () => {
        lang = button.dataset.lang;
        saved.set(LANG_KEY, lang);
        // The pick is remembered now; a ?lang left in the address would undo it on reload.
        const url = new URL(location.href);
        if (url.searchParams.has('lang')) {
          url.searchParams.delete('lang');
          history.replaceState(history.state, '', url);
        }
        translate();
        render();
      }),
    );
    $$('[data-filter]').forEach((button) =>
      button.addEventListener('click', () => {
        filter.platform = button.dataset.filter;
        render();
      }),
    );
    $('[data-search]').addEventListener('input', (event) => {
      filter.query = event.target.value;
      render();
    });
    $('[data-retry]').addEventListener('click', load);
    $('[data-apps]').addEventListener('click', async (event) => {
      const button = event.target.closest('.file__hash');
      if (!button) return;
      try {
        await navigator.clipboard.writeText(button.dataset.hash);
      } catch {
        return; // no clipboard here; the hash stays in the button's title
      }
      const use = button.querySelector('use');
      button.classList.add('is-copied');
      use.setAttribute('href', '#i-check');
      $('[data-announce]').textContent = t().copied;
      setTimeout(() => {
        button.classList.remove('is-copied');
        use.setAttribute('href', '#i-copy');
      }, 1600);
    });

    translate();
    Promise.all([
      load(),
      loadProducts().then(() => {
        if (apps) render();
      }),
      detectArch().then((arch) => {
        system.arch = arch;
        translate();
        render();
      }),
    ]).then(showLinkedApp);
  }

  setup();
})();
