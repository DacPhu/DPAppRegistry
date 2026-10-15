// The store at store.mitekbro.com. deploy.sh installs this as the page's config.js.
window.STORE_CONFIG = {
  name: 'Mitekbro Store',
  company: 'Mitekbro',
  logo: 'https://mitekbro.com/assets/img/favicon.svg',
  homeUrl: { vi: 'https://mitekbro.com/', en: 'https://mitekbro.com/en/' },
  api: '/api',
  channel: 'stable',
  owner: '',
  defaultLang: 'vi',
  tagline: {
    vi: 'Tải phiên bản mới nhất các ứng dụng của Mitekbro cho macOS, Windows và Linux.',
    en: 'Download the latest Mitekbro apps for macOS, Windows and Linux.',
  },
  about: {
    vi: 'Mitekbro đưa AI vào sản phẩm công nghệ và quy trình doanh nghiệp, trên nền tảng do chính chúng tôi xây dựng và vận hành.',
    en: 'Mitekbro builds AI into technology products and business workflows, on a stack we build and run ourselves.',
  },
  contact: 'pldphu@mitekbro.com',
  linksTitle: 'Mitekbro',
  links: [
    { label: { vi: 'Trang chủ', en: 'Home' }, url: { vi: 'https://mitekbro.com/', en: 'https://mitekbro.com/en/' } },
    { label: { vi: 'Sản phẩm', en: 'Products' }, url: { vi: 'https://mitekbro.com/#products', en: 'https://mitekbro.com/en/#products' } },
    {
      label: { vi: 'Giải pháp doanh nghiệp', en: 'Business solutions' },
      url: { vi: 'https://mitekbro.com/#business', en: 'https://mitekbro.com/en/#business' },
    },
  ],
  credit: false,
  // Copy from the Mitekbro site's product pages, keyed by registry app name.
  apps: {
    'llm-proxy': {
      title: 'LLM Proxy',
      tagline: {
        vi: 'Một điểm truy cập cho các tài khoản Claude và OpenAI của cả nhóm',
        en: 'One endpoint for your team’s Claude and OpenAI accounts',
      },
      description: {
        vi: 'Gom nhiều tài khoản Claude và OpenAI về sau một endpoint duy nhất và chia đều yêu cầu, để các công cụ AI và coding agent của cả nhóm luôn nằm trong giới hạn tốc độ của từng tài khoản.',
        en: 'Puts many Claude and OpenAI accounts behind a single endpoint and spreads requests between them, so a team’s AI tools and coding agents stay under each account’s rate limits.',
      },
      homepage: { vi: 'https://mitekbro.com/san-pham/llm-proxy/', en: 'https://mitekbro.com/en/products/llm-proxy/' },
    },
    dpfs: {
      title: 'DPFileSystem',
      tagline: {
        vi: 'Lưu trữ tương thích S3, chọn mã hóa theo từng bucket',
        en: 'S3-compatible storage with encryption chosen per bucket',
      },
      description: {
        vi: 'Bucket thường giữ mỗi đối tượng thành một file thật mà công cụ nào cũng đọc được; bucket mã hóa dùng AES-256-GCM. Mọi S3 client đều làm việc được với cả hai.',
        en: 'Plain buckets keep each object as a real file any tool can read; encrypted buckets use AES-256-GCM. Every S3 client works with both.',
      },
      // Prices come from the registry (GET /store/products); these are the words.
      pricing: {
        features: {
          community: {
            vi: ['Dùng cá nhân, phi thương mại', '1 máy chủ', 'Đầy đủ tính năng lưu trữ', 'Cập nhật tự nguyện'],
            en: ['Personal, non-commercial use', '1 server', 'Every storage feature', 'Updates at our discretion'],
          },
          business: {
            vi: ['Dùng thương mại', 'Mỗi license cho số máy chủ bạn chọn', 'Liên kết console nhiều máy chủ', 'Cập nhật và hỗ trợ email 1 năm'],
            en: ['Commercial use', 'One license for the servers you choose', 'Console federation across servers', 'A year of updates and email support'],
          },
          enterprise: {
            vi: ['Cam kết SLA', 'Hỗ trợ tại chỗ', 'Nhiều máy chủ, hợp đồng riêng'],
            en: ['Service level agreement', 'On-site support', 'Many servers, under contract'],
          },
        },
      },
    },
    'latex-tool': {
      title: 'Latex Tool',
      tagline: {
        vi: 'Trình soạn LaTeX cộng tác trên trình duyệt và máy tính',
        en: 'A collaborative LaTeX editor for the browser and the desktop',
      },
      description: {
        vi: 'Cùng viết tài liệu LaTeX nhiều file theo thời gian thực, biên dịch ra PDF trên máy chủ hoặc ngay trên laptop, và vẫn cộng tác được khi không có internet.',
        en: 'Write multi-file LaTeX documents together in real time, compile to PDF on a server or on your own laptop, and keep collaborating without an internet connection.',
      },
      homepage: { vi: 'https://mitekbro.com/san-pham/latex-tool/', en: 'https://mitekbro.com/en/products/latex-tool/' },
    },
  },
};
