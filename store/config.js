// Settings for this store page. Each deployment can ship its own copy of this
// file (deploy/ does), so the page itself never needs editing. Texts can be
// one string, or { vi: '…', en: '…' } to differ by language.
window.STORE_CONFIG = {
  // Shown in the header, the footer and the browser tab.
  name: 'App Store',
  // Who the copyright line names; defaults to the name.
  company: '',
  // An image URL for the brand mark and the tab icon; empty shows the name's initial.
  logo: '',
  // Where the brand links to; leave empty for no link.
  homeUrl: '',
  // Where the DPAppRegistry API is reachable from this page.
  api: '/api',
  // Which release channel the store shows.
  channel: 'stable',
  // Whose apps to list; empty means the server's STORE_OWNER.
  owner: '',
  // 'vi' or 'en', used until the visitor picks one.
  defaultLang: 'en',
  tagline: {
    en: 'Download the latest version of each app.',
    vi: 'Tải phiên bản mới nhất của từng ứng dụng.',
  },
  // A sentence about you under the footer's brand.
  about: '',
  // An email address for the help box and the footer.
  contact: '',
  // Footer links: [{ label, url }], under the heading linksTitle.
  linksTitle: '',
  links: [],
  // The "Served by DPAppRegistry" line in the footer.
  credit: true,
  // What to show for an app beyond what the registry holds, by its registry
  // name: { title, tagline, description, homepage, logo, pricing }. Prices
  // come from the registry's catalog (GET /store/products); pricing.features
  // holds the bullet points per plan: { business: { vi: [...], en: [...] } }.
  apps: {},
};
