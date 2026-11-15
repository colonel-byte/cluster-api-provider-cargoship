// Populate the sidebar
//
// This is a script, and not included directly in the page, to control the total size of the book.
// The TOC contains an entry for each page, so if each page includes a copy of the TOC,
// the total size of the page becomes O(n**2).
class MDBookSidebarScrollbox extends HTMLElement {
    constructor() {
        super();
    }
    connectedCallback() {
        this.innerHTML = '<ol class="chapter"><li class="chapter-item expanded "><span class="chapter-link-wrapper"><a href="index.html">Introduction</a></span></li><li class="chapter-item expanded "><li class="part-title">Guides</li></li><li class="chapter-item expanded "><span class="chapter-link-wrapper"><a href="guides/clusterclass.html">clusterclass</a></span></li><li class="chapter-item expanded "><span class="chapter-link-wrapper"><a href="guides/install.html">install</a></span></li><li class="chapter-item expanded "><span class="chapter-link-wrapper"><a href="guides/node-removal.html">node-removal</a></span></li><li class="chapter-item expanded "><span class="chapter-link-wrapper"><a href="guides/rancher-turtles.html">rancher-turtles</a></span></li><li class="chapter-item expanded "><span class="chapter-link-wrapper"><a href="guides/registry.html">registry</a></span></li><li class="chapter-item expanded "><li class="part-title">Development</li></li><li class="chapter-item expanded "><span class="chapter-link-wrapper"><a href="dev/e2e-tests.html">e2e-tests</a></span></li><li class="chapter-item expanded "><span class="chapter-link-wrapper"><a href="dev/manual-verification.html">manual-verification</a></span></li><li class="chapter-item expanded "><li class="part-title">Decision records</li></li><li class="chapter-item expanded "><span class="chapter-link-wrapper"><a href="agent/choice-clusterclass-template.html">choice-clusterclass-template</a></span></li><li class="chapter-item expanded "><span class="chapter-link-wrapper"><a href="agent/choice-distro-pinning.html">choice-distro-pinning</a></span></li><li class="chapter-item expanded "><span class="chapter-link-wrapper"><a href="agent/choice-in-memory-ssh-key.html">choice-in-memory-ssh-key</a></span></li><li class="chapter-item expanded "><span class="chapter-link-wrapper"><a href="agent/choice-makefile-not-mage.html">choice-makefile-not-mage</a></span></li><li class="chapter-item expanded "><span class="chapter-link-wrapper"><a href="agent/choice-no-rollout.html">choice-no-rollout</a></span></li><li class="chapter-item expanded "><span class="chapter-link-wrapper"><a href="agent/choice-one-converge.html">choice-one-converge</a></span></li><li class="chapter-item expanded "><span class="chapter-link-wrapper"><a href="agent/choice-osv-vendor-overrides.html">choice-osv-vendor-overrides</a></span></li><li class="chapter-item expanded "><span class="chapter-link-wrapper"><a href="agent/choice-record-discovery.html">choice-record-discovery</a></span></li><li class="chapter-item expanded "><span class="chapter-link-wrapper"><a href="agent/choice-registry-credentials.html">choice-registry-credentials</a></span></li><li class="chapter-item expanded "><span class="chapter-link-wrapper"><a href="agent/choice-tool-dependencies.html">choice-tool-dependencies</a></span></li><li class="chapter-item expanded "><span class="chapter-link-wrapper"><a href="agent/choice-zarf-package.html">choice-zarf-package</a></span></li><li class="chapter-item expanded "><span class="chapter-link-wrapper"><a href="agent/todo-clusterclass-support.html">todo-clusterclass-support</a></span></li><li class="chapter-item expanded "><span class="chapter-link-wrapper"><a href="agent/todo-e2e-fleet-backends.html">todo-e2e-fleet-backends</a></span></li></ol>';
        // Set the current, active page, and reveal it if it's hidden
        let current_page = document.location.href.toString().split('#')[0].split('?')[0];
        if (current_page.endsWith('/')) {
            current_page += 'index.html';
        }
        const links = Array.prototype.slice.call(this.querySelectorAll('a'));
        const l = links.length;
        for (let i = 0; i < l; ++i) {
            const link = links[i];
            const href = link.getAttribute('href');
            if (href && !href.startsWith('#') && !/^(?:[a-z+]+:)?\/\//.test(href)) {
                link.href = path_to_root + href;
            }
            // The 'index' page is supposed to alias the first chapter in the book.
            // Check both with and without the '.html' suffix to be robust against pretty URLs
            if (link.href.replace(/\.html$/, '') === current_page.replace(/\.html$/, '')
                || i === 0
                && path_to_root === ''
                && current_page.endsWith('/index.html')) {
                link.classList.add('active');
                let parent = link.parentElement;
                while (parent) {
                    if (parent.tagName === 'LI' && parent.classList.contains('chapter-item')) {
                        parent.classList.add('expanded');
                    }
                    parent = parent.parentElement;
                }
            }
        }
        // Track and set sidebar scroll position
        this.addEventListener('click', e => {
            if (e.target.tagName === 'A') {
                const clientRect = e.target.getBoundingClientRect();
                const sidebarRect = this.getBoundingClientRect();
                sessionStorage.setItem('sidebar-scroll-offset', clientRect.top - sidebarRect.top);
            }
        }, { passive: true });
        const sidebarScrollOffset = sessionStorage.getItem('sidebar-scroll-offset');
        sessionStorage.removeItem('sidebar-scroll-offset');
        if (sidebarScrollOffset !== null) {
            // preserve sidebar scroll position when navigating via links within sidebar
            const activeSection = this.querySelector('.active');
            if (activeSection) {
                const clientRect = activeSection.getBoundingClientRect();
                const sidebarRect = this.getBoundingClientRect();
                const currentOffset = clientRect.top - sidebarRect.top;
                this.scrollTop += currentOffset - parseFloat(sidebarScrollOffset);
            }
        } else {
            // scroll sidebar to current active section when navigating via
            // 'next/previous chapter' buttons
            const activeSection = document.querySelector('#mdbook-sidebar .active');
            if (activeSection) {
                activeSection.scrollIntoView({ block: 'center' });
            }
        }
        // Toggle buttons
        const sidebarAnchorToggles = document.querySelectorAll('.chapter-fold-toggle');
        function toggleSection(ev) {
            ev.currentTarget.parentElement.parentElement.classList.toggle('expanded');
        }
        Array.from(sidebarAnchorToggles).forEach(el => {
            el.addEventListener('click', toggleSection);
        });
    }
}
window.customElements.define('mdbook-sidebar-scrollbox', MDBookSidebarScrollbox);

