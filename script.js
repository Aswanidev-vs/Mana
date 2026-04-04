// ===== Sidebar Active Link Tracking =====
document.addEventListener('DOMContentLoaded', () => {
  const sections = document.querySelectorAll('section[id]');
  const sidebarLinks = document.querySelectorAll('.sidebar-nav a');
  
  const observer = new IntersectionObserver((entries) => {
    entries.forEach(entry => {
      if (entry.isIntersecting) {
        const id = entry.target.id;
        sidebarLinks.forEach(link => {
          link.classList.toggle('active', link.getAttribute('href') === '#' + id);
        });
      }
    });
  }, { rootMargin: '-80px 0px -60% 0px', threshold: 0 });

  sections.forEach(section => observer.observe(section));

  // ===== Topnav Scroll Effect =====
  const topnav = document.getElementById('topnav');
  let lastScroll = 0;
  window.addEventListener('scroll', () => {
    const scrollY = window.scrollY;
    topnav.style.borderBottomColor = scrollY > 50 ? 'var(--border)' : 'transparent';
    lastScroll = scrollY;
  }, { passive: true });

  // ===== Code Blocks: Auto-Highlighter & Copy Hub =====
  document.querySelectorAll('pre').forEach(pre => {
    // 0. Only process each block once to prevent artifacts
    if (pre.dataset.processed) return;
    pre.dataset.processed = "true";

    // 1. Create Wrapper for Copy Button
    const wrapper = document.createElement('div');
    wrapper.className = 'code-wrapper';
    pre.parentNode.insertBefore(wrapper, pre);
    wrapper.appendChild(pre);

    // 2. Inject Copy Button
    const copyBtn = document.createElement('button');
    copyBtn.className = 'copy-btn';
    copyBtn.setAttribute('title', 'Copy to Clipboard');
    copyBtn.innerHTML = `<svg viewBox="0 0 24 24"><path d="M8 4H6C4.89543 4 4 4.89543 4 6V16C4 17.1046 4.89543 18 6 18H12C13.1046 18 14 17.1046 14 16V14M8 4C8 5.10457 8.89543 6 10 6C11.1046 6 12 5.10457 12 4M8 4C8 2.89543 8.89543 2 10 2C11.1046 2 12 2.89543 12 4M12 4H14C15.1046 4 16 4.89543 16 6V10"></path></svg>`;
    wrapper.appendChild(copyBtn);

    // 3. Copy Handler
    copyBtn.addEventListener('click', () => {
      const code = pre.querySelector('code').innerText;
      navigator.clipboard.writeText(code).then(() => {
        copyBtn.classList.add('copied');
        copyBtn.innerHTML = `<svg viewBox="0 0 24 24"><path d="M20 6L9 17L4 12"></path></svg>`;
        setTimeout(() => {
          copyBtn.classList.remove('copied');
          copyBtn.innerHTML = `<svg viewBox="0 0 24 24"><path d="M8 4H6C4.89543 4 4 4.89543 4 6V16C4 17.1046 4.89543 18 6 18H12C13.1046 18 14 17.1046 14 16V14M8 4C8 5.10457 8.89543 6 10 6C11.1046 6 12 5.10457 12 4M8 4C8 2.89543 8.89543 2 10 2C11.1046 2 12 2.89543 12 4M12 4H14C15.1046 4 16 4.89543 16 6V10"></path></svg>`;
        }, 2000);
      });
    });

    // 4. Highlight Logic
    const codeBlock = pre.querySelector('code');
    if (codeBlock) highlightBlock(codeBlock);
  });

  // ===== Smooth sidebar scroll on click =====
  sidebarLinks.forEach(link => {
    link.addEventListener('click', (e) => {
      const href = link.getAttribute('href');
      if (href.startsWith('#')) {
        e.preventDefault();
        const target = document.querySelector(href);
        if (target) {
          target.scrollIntoView({ behavior: 'smooth', block: 'start' });
          history.pushState(null, '', href);
        }
      }
    });
  });
});

function highlightBlock(block) {
  // 1. Get raw text and escape basic HTML characters
  let text = block.textContent
    .replace(/&/g, "&amp;")
    .replace(/</g, "&lt;")
    .replace(/>/g, "&gt;");

  const isBash = block.classList.contains('lang-bash');
  const isJson = block.classList.contains('lang-json');

  let html = "";
  if (isJson) {
    // Single-pass JSON (Keywords/Strings/Numbers/Types)
    const jsonRegex = /(".*?")\s*:|:(".*?")|(\b\d+\b)|(\btrue|false|null\b)/g;
    html = text.replace(jsonRegex, (match, key, val, num, typ) => {
      if (key) return `<span class="tk">${key}</span>:`;
      if (val) return `:<span class="ts">${val}</span>`;
      if (num) return `<span class="tn">${num}</span>`;
      if (typ) return `<span class="tt">${typ}</span>`;
      return match;
    });
  } else if (isBash) {
    // Single-pass Bash (Functions/Strings)
    const bashRegex = /\b(go\s+\w+|curl|python|npx|npm|mkdir|ls|cat|cd)\b|(".*?")/g;
    html = text.replace(bashRegex, (match, fn, str) => {
      if (fn) return `<span class="tf">${fn}</span>`;
      if (str) return `<span class="ts">${str}</span>`;
      return match;
    });
  } else {
    // Single-pass Go (Comments/Strings/Keywords/Types/Numbers)
    // IMPORTANT: This regex ensures that once a comment or string is matched, its contents are not re-processed.
    const goRegex = /(\/\/.*)|(".*?")|\b(package|import|func|type|struct|interface|return|if|else|for|range|switch|case|default|var|const|defer|go|select|chan|map|make|nil|true|false|error|break|continue)\b|\b(string|int|int64|uint64|bool|byte|float64|time\.Duration|time\.Time|context\.Context)\b|(\b\d+\b)/g;
    
    html = text.replace(goRegex, (match, cm, str, kw, ty, nu) => {
      if (cm) return `<span class="tc">${cm}</span>`;
      if (str) return `<span class="ts">${str}</span>`;
      if (kw) return `<span class="tk">${kw}</span>`;
      if (ty) return `<span class="tt">${ty}</span>`;
      if (nu) return `<span class="tn">${nu}</span>`;
      return match;
    });
  }

  // 2. Commit the clean result
  block.innerHTML = html;
}
