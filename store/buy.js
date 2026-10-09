// The store's checkout: a plan is picked, an order is made (POST
// <api>/store/orders), the buyer pays by bank transfer with the QR and the
// payment code, the page polls the order until the transfer arrives and the
// registry has issued the license, then shows the key. /order/<id>?t=<token>
// brings the buyer back to the same order.

(() => {
  const config = window.STORE_CONFIG || {};
  const api = config.api || '/api';

  const STRINGS = {
    en: {
      title: 'Buy a license',
      plan: 'Plan',
      quantity: 'Servers',
      quantityHelp: 'One license covers this many servers (clusters).',
      name: 'Name or company',
      email: 'Email',
      emailHelp: 'The license key is shown here and sent to this address.',
      company: 'Company (optional)',
      eula: 'I accept the <a href="/eula.html" target="_blank" rel="noopener">license agreement (EULA)</a>.',
      total: 'Total',
      create: 'Continue to payment',
      creating: 'Creating the order…',
      cancel: 'Cancel',
      close: 'Close',
      back: 'Back',
      payTitle: 'Pay by bank transfer',
      payLead: 'Scan the QR with your banking app, or transfer by hand with exactly this content. The key appears here as soon as the transfer arrives, usually within a minute.',
      amount: 'Amount',
      content: 'Transfer content',
      bank: 'Bank',
      account: 'Account number',
      holder: 'Account holder',
      copy: 'Copy',
      copied: 'Copied',
      waiting: 'Waiting for the transfer…',
      expired: 'This order is older than 24 hours. A transfer that arrives later still completes it; or start a new order.',
      paid: 'Paid. Your license is ready.',
      orderLink: 'Order page',
      keepOpen: 'Keep this page open, or come back with this link:',
      doneTitle: 'Your license',
      doneLead: 'Paste the key as one line. It is also on the order page',
      sentTo: 'and was emailed to {email}',
      install: 'Install it on the server:',
      copyKey: 'Copy the key',
      notFound: 'This order could not be found. Check the link, or start a new order.',
      failed: 'Something went wrong: {error}',
      network: 'The store could not be reached. Check your connection and try again.',
      fieldName: 'Enter a name of at least 2 characters.',
      fieldEmail: 'Enter a valid email address.',
      fieldEula: 'Please accept the license agreement.',
      perServerYear: '/ server / year',
      servers: '{n} server(s)',
      validUntil: 'Valid, with updates, until {date}',
    },
    vi: {
      title: 'Mua license',
      plan: 'Gói',
      quantity: 'Số máy chủ',
      quantityHelp: 'Một license dùng cho số máy chủ (cluster) này.',
      name: 'Tên hoặc công ty',
      email: 'Email',
      emailHelp: 'License key hiện ở đây và được gửi tới địa chỉ này.',
      company: 'Công ty (không bắt buộc)',
      eula: 'Tôi đồng ý với <a href="/eula.html" target="_blank" rel="noopener">thỏa thuận cấp phép (EULA)</a>.',
      total: 'Tổng',
      create: 'Tiếp tục thanh toán',
      creating: 'Đang tạo đơn hàng…',
      cancel: 'Hủy',
      close: 'Đóng',
      back: 'Quay lại',
      payTitle: 'Thanh toán chuyển khoản',
      payLead: 'Quét mã QR bằng app ngân hàng, hoặc chuyển khoản thủ công với đúng nội dung này. Key sẽ hiện ở đây ngay khi tiền về, thường trong vòng một phút.',
      amount: 'Số tiền',
      content: 'Nội dung chuyển khoản',
      bank: 'Ngân hàng',
      account: 'Số tài khoản',
      holder: 'Chủ tài khoản',
      copy: 'Chép',
      copied: 'Đã chép',
      waiting: 'Đang chờ chuyển khoản…',
      expired: 'Đơn hàng này đã quá 24 giờ. Chuyển khoản đến muộn vẫn hoàn tất đơn; hoặc bạn tạo đơn mới.',
      paid: 'Đã thanh toán. License của bạn đã sẵn sàng.',
      orderLink: 'Trang đơn hàng',
      keepOpen: 'Giữ trang này mở, hoặc quay lại bằng liên kết:',
      doneTitle: 'License của bạn',
      doneLead: 'Dán key nguyên một dòng. Key cũng có trên trang đơn hàng',
      sentTo: 'và đã được gửi tới {email}',
      install: 'Cài trên máy chủ:',
      copyKey: 'Chép key',
      notFound: 'Không tìm thấy đơn hàng này. Hãy kiểm tra liên kết, hoặc tạo đơn mới.',
      failed: 'Có lỗi xảy ra: {error}',
      network: 'Không kết nối được tới store. Hãy kiểm tra mạng rồi thử lại.',
      fieldName: 'Nhập tên có ít nhất 2 ký tự.',
      fieldEmail: 'Nhập địa chỉ email hợp lệ.',
      fieldEula: 'Bạn cần đồng ý với thỏa thuận cấp phép.',
      perServerYear: '/ máy chủ / năm',
      servers: '{n} máy chủ',
      validUntil: 'Hiệu lực và cập nhật đến {date}',
    },
  };

  const $ = (selector, root = document) => root.querySelector(selector);
  const fill = (text, values) => text.replace(/\{(\w+)\}/g, (_, key) => values[key] ?? '');
  const lang = () => (STRINGS[document.documentElement.lang] ? document.documentElement.lang : 'en');
  const t = () => STRINGS[lang()];
  const vnd = (n) => new Intl.NumberFormat('vi-VN', { style: 'currency', currency: 'VND', maximumFractionDigits: 0 }).format(n);
  const day = (value) => new Intl.DateTimeFormat(lang() === 'vi' ? 'vi-VN' : 'en-GB', { dateStyle: 'medium' }).format(new Date(value));

  const dialog = $('#buy-dialog');
  if (!dialog) return;
  const panels = { form: $('[data-panel="form"]', dialog), pay: $('[data-panel="pay"]', dialog), done: $('[data-panel="done"]', dialog) };
  const state = { product: null, plan: null, order: null, token: '', timer: 0, started: 0 };

  function show(panel) {
    for (const [name, node] of Object.entries(panels)) node.hidden = name !== panel;
    translate();
    if (!dialog.open) dialog.showModal();
  }

  function translate() {
    const s = t();
    dialog.querySelectorAll('[data-bt]').forEach((node) => {
      if (s[node.dataset.bt]) node.textContent = s[node.dataset.bt];
    });
    $('[data-bt-html="eula"]', dialog).innerHTML = s.eula; // the string is ours, not the server's
  }

  // The form

  function open({ product, plan, title }) {
    state.product = product;
    state.plan = plan;
    state.order = null;
    stopPolling();
    const s = t();
    $('[data-buy-product]', dialog).textContent = title || product.name;
    $('[data-buy-plan]', dialog).textContent = `${plan.name} · ${vnd(plan.vnd)} ${plan.per === 'cluster' ? s.perServerYear : ''}`;
    const quantity = $('[data-buy-quantity]', dialog);
    quantity.value = '1';
    quantity.max = String(plan.max_quantity || 1);
    $('[data-buy-quantity-row]', dialog).hidden = !plan.per;
    $('[data-buy-error]', dialog).hidden = true;
    $('[data-buy-submit]', dialog).disabled = false;
    updateTotal();
    show('form');
    $('[data-buy-name]', dialog).focus();
  }

  function updateTotal() {
    const quantity = Math.max(1, Math.min(Number($('[data-buy-quantity]', dialog).value) || 1, state.plan?.max_quantity || 1));
    $('[data-buy-quantity]', dialog).value = String(quantity);
    $('[data-buy-total]', dialog).textContent = vnd((state.plan?.vnd || 0) * quantity);
  }

  function fail(message) {
    const error = $('[data-buy-error]', dialog);
    error.textContent = message;
    error.hidden = false;
  }

  async function submit(event) {
    event.preventDefault();
    const s = t();
    const name = $('[data-buy-name]', dialog).value.trim();
    const email = $('[data-buy-email]', dialog).value.trim();
    const company = $('[data-buy-company]', dialog).value.trim();
    if (name.length < 2) return fail(s.fieldName);
    if (!/^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(email)) return fail(s.fieldEmail);
    if (!$('[data-buy-eula]', dialog).checked) return fail(s.fieldEula);
    $('[data-buy-error]', dialog).hidden = true;
    const button = $('[data-buy-submit]', dialog);
    button.disabled = true;
    button.textContent = s.creating;
    try {
      const response = await fetch(`${api}/store/orders`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json', Accept: 'application/json' },
        body: JSON.stringify({
          product: state.product.id,
          plan: state.plan.id,
          quantity: Number($('[data-buy-quantity]', dialog).value) || 1,
          customer: { name, email, company },
          lang: lang(),
        }),
      });
      const body = await response.json().catch(() => ({}));
      if (!response.ok) throw new Error(body.error || `HTTP ${response.status}`);
      state.order = body;
      state.token = body.view_token;
      history.replaceState(null, '', `/order/${body.id}?t=${encodeURIComponent(body.view_token)}`);
      renderPayment();
      startPolling();
    } catch (error) {
      fail(error instanceof TypeError ? s.network : fill(s.failed, { error: error.message }));
    } finally {
      button.disabled = false;
      button.textContent = s.create;
    }
  }

  // Payment

  function renderPayment() {
    const o = state.order;
    const s = t();
    $('[data-pay-qr]', dialog).src = o.qr_url;
    $('[data-pay-amount]', dialog).textContent = vnd(o.amount_vnd);
    $('[data-pay-code]', dialog).textContent = o.payment_code;
    $('[data-pay-bank]', dialog).textContent = o.bank?.name || '';
    $('[data-pay-account]', dialog).textContent = o.bank?.account || '';
    $('[data-pay-holder]', dialog).textContent = o.bank?.holder || '';
    $('[data-pay-holder-row]', dialog).hidden = !o.bank?.holder;
    const link = $('[data-pay-link]', dialog);
    link.href = location.pathname + location.search;
    link.textContent = location.origin + location.pathname;
    renderStatus();
    show(o.status === 'paid' && o.license ? 'done' : 'pay');
    if (o.status === 'paid' && o.license) renderDone();
  }

  function renderStatus() {
    const o = state.order;
    const s = t();
    const status = $('[data-pay-status]', dialog);
    status.className = `status status--${o.status}`;
    status.textContent = o.status === 'paid' ? s.paid : o.status === 'expired' ? s.expired : s.waiting;
  }

  function renderDone() {
    const o = state.order;
    const s = t();
    $('[data-done-key]', dialog).value = o.license.key;
    $('[data-done-install]', dialog).textContent = `dpfs license install '${o.license.key}'`;
    $('[data-done-lead]', dialog).textContent = `${s.doneLead}${o.email_sent ? ' ' + fill(s.sentTo, { email: o.email }) : ''}.`;
    $('[data-done-terms]', dialog).textContent = `${fill(s.servers, { n: o.license.clusters || o.quantity })} · ${fill(s.validUntil, { date: day(o.license.expires_at) })}`;
    const link = $('[data-done-link]', dialog);
    link.href = location.pathname + location.search;
    link.textContent = location.origin + location.pathname;
  }

  // Polling

  async function poll() {
    if (!state.order) return;
    try {
      const response = await fetch(`${api}/store/orders/${encodeURIComponent(state.order.id)}?t=${encodeURIComponent(state.token)}`, { headers: { Accept: 'application/json' } });
      if (response.status === 404) {
        stopPolling();
        fail(t().notFound);
        show('form');
        return;
      }
      if (!response.ok) return;
      const previous = state.order.status;
      state.order = { ...state.order, ...(await response.json()) };
      if (state.order.status === 'paid' && state.order.license) {
        stopPolling();
        renderDone();
        show('done');
      } else if (state.order.status !== previous) {
        renderStatus();
      }
    } catch {
      // Offline for a moment; the next poll tries again.
    }
  }

  function startPolling() {
    stopPolling();
    state.started = Date.now();
    const tick = () => {
      if (!document.hidden) void poll();
      const elapsed = Date.now() - state.started;
      const delay = state.order?.status === 'expired' ? 30000 : elapsed > 120000 ? 10000 : 5000;
      state.timer = window.setTimeout(tick, delay);
    };
    state.timer = window.setTimeout(tick, 5000);
    document.addEventListener('visibilitychange', onVisible);
  }

  function onVisible() {
    if (!document.hidden) void poll();
  }

  function stopPolling() {
    window.clearTimeout(state.timer);
    state.timer = 0;
    document.removeEventListener('visibilitychange', onVisible);
  }

  // Coming back with the link

  async function resume({ id, token }) {
    state.token = token;
    const s = t();
    try {
      const response = await fetch(`${api}/store/orders/${encodeURIComponent(id)}?t=${encodeURIComponent(token)}`, { headers: { Accept: 'application/json' } });
      if (!response.ok) throw new Error(response.status === 404 ? s.notFound : `HTTP ${response.status}`);
      state.order = await response.json();
      state.product = { id: state.order.product };
      state.plan = { id: state.order.plan };
      renderPayment();
      if (!(state.order.status === 'paid' && state.order.license)) startPolling();
    } catch (error) {
      fail(error instanceof TypeError ? s.network : error.message);
      show('form');
      panels.form.querySelector('form').hidden = true;
    }
  }

  // Wiring

  function copyFrom(button) {
    const target = $(button.dataset.copy, dialog);
    const text = target.value ?? target.textContent;
    navigator.clipboard?.writeText(text).then(() => {
      const label = button.textContent;
      button.textContent = t().copied;
      button.classList.add('is-copied');
      setTimeout(() => {
        button.textContent = label;
        button.classList.remove('is-copied');
      }, 1600);
    });
  }

  panels.form.querySelector('form').addEventListener('submit', submit);
  $('[data-buy-quantity]', dialog).addEventListener('input', updateTotal);
  dialog.querySelectorAll('[data-copy]').forEach((button) => button.addEventListener('click', () => copyFrom(button)));
  dialog.querySelectorAll('[data-close]').forEach((button) =>
    button.addEventListener('click', () => {
      dialog.close();
    }),
  );
  dialog.addEventListener('close', () => {
    // A paid or pending order keeps its link in the address bar; polling stops with the sheet.
    stopPolling();
    panels.form.querySelector('form').hidden = false;
  });
  document.querySelectorAll('[data-lang]').forEach((button) => button.addEventListener('click', () => setTimeout(() => dialog.open && translate(), 0)));

  window.StoreBuy = { open, resume };

  const order = location.pathname.match(/^\/order\/([a-f0-9]{24})\/?$/);
  if (order) {
    const token = new URLSearchParams(location.search).get('t') || '';
    resume({ id: order[1], token });
  }
})();
