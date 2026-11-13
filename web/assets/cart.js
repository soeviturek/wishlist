'use strict';

const byId = id => document.getElementById(id);
const SEARCH_HELP = 'Search your wishlist and products previously added by link, even after removing them.';
let activeEmail = '';
let cartItems = [];
let cartLoaded = false;
let loading = false;
let busy = false;
let loadSequence = 0;
let searchTimer;
let searchController;
let savedProducts = [];
let imageController;
let preparedImage;
let preparedFile;
let previewURL = '';
let textExporting = false;

const emailQuery = () => `email=${encodeURIComponent(activeEmail)}`;
const money = cents => `$${(cents / 100).toFixed(2)}`;

function totals(items) {
  return items.reduce((total, item) => {
    total.quantity += item.quantity;
    if (item.current_price == null) total.unknown += item.quantity;
    else total.cents += Math.round(item.current_price * 100) * item.quantity;
    return total;
  }, { quantity: 0, unknown: 0, cents: 0 });
}

function httpURL(value, base = window.location.origin) {
  if (!value || !URL.canParse(value, base)) return '';
  const url = new URL(value, base);
  return ['https:', 'http:'].includes(url.protocol) ? url.href : '';
}

function photoMarkup(product) {
  const url = httpURL(product.image_url, product.url);
  return url
    ? `<img class="cart-product-photo" src="${esc(url)}" alt="" loading="lazy" decoding="async" data-product-photo>`
    : '<div class="cart-product-photo photo-placeholder" role="img" aria-label="Product photo unavailable">No photo</div>';
}

document.addEventListener('error', event => {
  if (!(event.target instanceof HTMLImageElement) || !event.target.hasAttribute('data-product-photo')) return;
  const placeholder = document.createElement('div');
  placeholder.className = 'cart-product-photo photo-placeholder';
  placeholder.setAttribute('role', 'img');
  placeholder.setAttribute('aria-label', 'Product photo unavailable');
  placeholder.textContent = 'No photo';
  event.target.replaceWith(placeholder);
}, true);

function syncControls() {
  const locked = busy || loading;
  document.querySelectorAll('[data-cart-write]').forEach(element => {
    element.disabled = locked || element.dataset.boundary === 'true';
  });
  byId('cartEmail').disabled = locked;
  byId('loadCartBtn').disabled = locked;
  byId('shareCartBtn').disabled = locked || !cartItems.length;
  byId('exportTextBtn').disabled = locked || !cartItems.length || textExporting;
  byId('dialogTextBtn').disabled = !cartItems.length || textExporting;
  byId('cartItems').setAttribute('aria-busy', String(busy));
}

function renderCart() {
  const summary = totals(cartItems);
  const groups = new Map();
  for (const item of cartItems) {
    if (!groups.has(item.store)) groups.set(item.store, []);
    groups.get(item.store).push(item);
  }
  byId('cartSummary').hidden = !cartItems.length;
  byId('cartCount').textContent = `${cartItems.length} products | ${summary.quantity} items | ${groups.size} stores`;
  byId('totalLabel').textContent = summary.unknown ? 'Known subtotal' : 'Estimated total';
  byId('cartTotal').textContent = money(summary.cents);
  byId('unknownPriceNote').hidden = !summary.unknown;
  byId('unknownPriceNote').textContent = `${summary.unknown} item(s) have no recorded price and are not included in the subtotal.`;
  if (!cartItems.length) {
    byId('cartItems').innerHTML = '<div class="empty-state">Your cart is empty. Paste a link, search saved products or add an item from your wishlist.</div>';
    syncControls();
    return;
  }
  byId('cartItems').innerHTML = [...groups].sort(([a], [b]) => a.localeCompare(b)).map(([store, items]) => {
    const subtotal = totals(items);
    const storeClass = { Woolworths: 'woolworths', 'Chemist Warehouse': 'chemist', iHerb: 'iherb' }[store] || '';
    return `<section class="store-section" aria-label="${esc(store)}">
      <div class="store-heading">
        <h3><span class="store-dot ${storeClass}"></span>${esc(store)}</h3>
        <span class="store-subtotal">${subtotal.quantity} items | ${money(subtotal.cents)}${subtotal.unknown ? ' + unknown' : ''}</span>
      </div>
      ${items.map(item => {
        const url = httpURL(item.url);
        const name = url
          ? `<a class="cart-item-name" href="${esc(url)}" target="_blank" rel="noopener noreferrer">${esc(item.name)}</a>`
          : `<div class="cart-item-name">${esc(item.name)}</div>`;
        const unit = item.current_price == null ? 'Price unavailable' : `${money(Math.round(item.current_price * 100))} each`;
        const line = item.current_price == null ? 'Unknown' : money(Math.round(item.current_price * 100) * item.quantity);
        const date = item.price_updated_at ? `Price checked ${new Date(item.price_updated_at).toLocaleDateString()}` : 'No price recorded yet';
        return `<article class="cart-item" data-product-id="${esc(item.id)}">
          ${photoMarkup(item)}
          <div>${name}<div class="cart-item-price">${unit}</div><div class="cart-price-date">${esc(date)}</div></div>
          <div class="cart-row-end">
            <div class="cart-item-total">${line}</div>
            <div class="quantity-controls">
              <button type="button" data-action="decrease" data-cart-write data-boundary="${item.quantity <= 1}" aria-label="Decrease quantity for ${esc(item.name)}">-</button>
              <input type="number" min="1" max="999" step="1" value="${item.quantity}" data-action="quantity" data-cart-write aria-label="Quantity for ${esc(item.name)}">
              <button type="button" data-action="increase" data-cart-write data-boundary="${item.quantity >= 999}" aria-label="Increase quantity for ${esc(item.name)}">+</button>
            </div>
            <button type="button" class="cart-remove" data-action="remove" data-cart-write aria-label="Remove ${esc(item.name)} from cart">Remove</button>
          </div>
        </article>`;
      }).join('')}
    </section>`;
  }).join('');
  syncControls();
}

function renderSavedProducts() {
  byId('searchResults').innerHTML = savedProducts.map(product => {
    const quantity = cartItems.find(item => item.id === product.id)?.quantity || 0;
    const price = product.current_price == null ? 'Price unavailable' : money(Math.round(product.current_price * 100));
    return `<div class="saved-product">
      ${photoMarkup(product)}
      <div>
        <div class="saved-product-name">${esc(product.name)}</div>
        <div class="saved-product-meta">${esc(product.store)} | ${price}${quantity ? ` | In cart: ${quantity}` : ''}</div>
      </div>
      <button type="button" class="btn btn-primary btn-small" data-saved-product="${esc(product.id)}" data-cart-write data-boundary="${quantity >= 999}" aria-label="Add ${esc(product.name)} to cart">${quantity ? 'Add +1' : 'Add'}</button>
    </div>`;
  }).join('');
  syncControls();
}

function resetPreparedImage() {
  imageController?.abort();
  preparedImage = null;
  preparedFile = null;
  if (previewURL) URL.revokeObjectURL(previewURL);
  previewURL = '';
  byId('sharePreview').removeAttribute('src');
  byId('sharePreviewFrame').hidden = true;
  byId('downloadImageBtn').disabled = true;
  byId('nativeShareBtn').disabled = true;
  byId('nativeShareBtn').hidden = true;
}

async function loadCart(email) {
  const sequence = ++loadSequence;
  loading = true;
  cartLoaded = false;
  cartItems = [];
  savedProducts = [];
  searchController?.abort();
  clearTimeout(searchTimer);
  byId('productSearch').value = '';
  byId('searchResults').replaceChildren();
  byId('searchStatus').textContent = SEARCH_HELP;
  byId('cartWorkspace').hidden = true;
  byId('accountStatus').classList.remove('error-text');
  byId('accountStatus').textContent = 'Loading your cart...';
  if (byId('shareDialog').open) byId('shareDialog').close();
  resetPreparedImage();
  syncControls();
  try {
    const items = await apiJSON(`/cart/items?email=${encodeURIComponent(email)}`);
    if (sequence !== loadSequence) return;
    activeEmail = email;
    cartItems = items;
    cartLoaded = true;
    localStorage.setItem('wt_email', email);
    byId('accountStatus').textContent = `Viewing the cart for ${email}. Changes are saved automatically.`;
    byId('cartWorkspace').hidden = false;
    renderCart();
  } catch (error) {
    if (sequence !== loadSequence) return;
    byId('accountStatus').classList.add('error-text');
    byId('accountStatus').textContent = error.message;
  } finally {
    if (sequence === loadSequence) {
      loading = false;
      syncControls();
    }
  }
}

async function mutateCart({ path = '/cart/items', method = 'POST', body, removeID, message }) {
  if (!cartLoaded || loading || busy) {
    toast('Please wait for your cart to finish saving.', 'error');
    return false;
  }
  const focused = document.activeElement;
  const focusedID = focused.closest('[data-product-id]')?.dataset.productId;
  const focusedAction = focused.dataset.action;
  busy = true;
  resetPreparedImage();
  syncControls();
  try {
    const result = await apiJSON(path, {
      method,
      ...(body ? { headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body) } : {})
    });
    if (removeID) {
      cartItems = cartItems.filter(item => item.id !== removeID);
    } else {
      const index = cartItems.findIndex(item => item.id === result.id);
      if (index < 0) cartItems.push(result);
      else cartItems[index] = result;
    }
    renderCart();
    renderSavedProducts();
    byId('cartUpdateStatus').textContent = message;
    toast(message);
    return true;
  } catch (error) {
    toast(error.message, 'error');
    renderCart();
    return false;
  } finally {
    busy = false;
    syncControls();
    if (focusedID && focusedAction) {
      byId('cartItems').querySelector(`[data-product-id="${CSS.escape(focusedID)}"] [data-action="${CSS.escape(focusedAction)}"]`)?.focus();
    }
  }
}

async function searchProducts() {
  searchController?.abort();
  const query = byId('productSearch').value.trim();
  savedProducts = [];
  renderSavedProducts();
  byId('searchStatus').classList.remove('error-text');
  if (!query || !cartLoaded) {
    byId('searchStatus').textContent = SEARCH_HELP;
    return;
  }
  const email = activeEmail;
  const controller = new AbortController();
  searchController = controller;
  byId('searchStatus').textContent = 'Searching saved products...';
  try {
    const products = await apiJSON(`/products?email=${encodeURIComponent(email)}&q=${encodeURIComponent(query)}`, { signal: controller.signal });
    if (controller.signal.aborted || email !== activeEmail || query !== byId('productSearch').value.trim()) return;
    savedProducts = products;
    byId('searchStatus').textContent = products.length
      ? `${products.length} match(es)${products.length === 30 ? '; refine your search for more' : ''}. Add a product without extracting its link again.`
      : 'No saved products match. Try another name or paste a product link.';
    renderSavedProducts();
  } catch (error) {
    if (error.name === 'AbortError') return;
    byId('searchStatus').classList.add('error-text');
    byId('searchStatus').textContent = error.message;
  }
}

function updateQuantity(id, quantity) {
  if (!Number.isInteger(quantity) || quantity < 1 || quantity > 999) {
    toast('Quantity must be a whole number between 1 and 999.', 'error');
    renderCart();
    return;
  }
  const item = cartItems.find(item => item.id === id);
  if (!item || quantity === item.quantity) return;
  return mutateCart({
    path: `/cart/items/${encodeURIComponent(id)}`, method: 'PATCH',
    body: { email: activeEmail, quantity }, message: `Quantity updated to ${quantity}.`
  });
}

function exportFilename(extension) {
  return `shopping-list-${new Date().toISOString().slice(0, 10)}.${extension}`;
}

function downloadBlob(blob, filename) {
  const url = URL.createObjectURL(blob);
  const link = document.createElement('a');
  link.href = url;
  link.download = filename;
  document.body.appendChild(link);
  link.click();
  link.remove();
  setTimeout(() => URL.revokeObjectURL(url), 60000);
}

async function exportText() {
  if (!cartItems.length || busy || textExporting) return;
  textExporting = true;
  syncControls();
  try {
    const response = await apiFetch(`/cart/export.txt?${emailQuery()}`);
    downloadBlob(await response.blob(), exportFilename('txt'));
    toast('Text shopping list downloaded.');
  } catch (error) {
    toast(error.message, 'error');
  } finally {
    textExporting = false;
    syncControls();
  }
}

async function openShare() {
  if (!cartItems.length || busy) return;
  resetPreparedImage();
  byId('shareStatus').classList.remove('error-text');
  byId('shareStatus').textContent = 'Preparing your shopping list and product photos...';
  byId('shareDialog').showModal();
  const controller = new AbortController();
  imageController = controller;
  try {
    const response = await apiFetch(`/cart/export.png?${emailQuery()}`, { signal: controller.signal });
    const blob = await response.blob();
    if (controller.signal.aborted || !byId('shareDialog').open) return;
    preparedImage = blob;
    preparedFile = new File([blob], exportFilename('png'), { type: 'image/png' });
    previewURL = URL.createObjectURL(blob);
    byId('sharePreview').src = previewURL;
    byId('sharePreviewFrame').hidden = false;
    byId('downloadImageBtn').disabled = false;
    if (navigator.canShare?.({ files: [preparedFile] }) && navigator.share) {
      byId('nativeShareBtn').hidden = false;
      byId('nativeShareBtn').disabled = false;
    }
    const missing = Number(response.headers.get('X-Cart-Missing-Photos') || 0);
    byId('shareStatus').textContent = missing
      ? `Your list is ready. ${missing} product photo(s) could not be loaded; all items, quantities and prices are included.`
      : 'Your list is ready. Share it or save the image to take shopping.';
  } catch (error) {
    if (error.name === 'AbortError') return;
    byId('shareStatus').classList.add('error-text');
    byId('shareStatus').textContent = `${error.message}. You can still export the text version.`;
  }
}

byId('cartAccountForm').addEventListener('submit', event => {
  event.preventDefault();
  loadCart(byId('cartEmail').value.trim());
});

byId('cartLinkForm').addEventListener('submit', async event => {
  event.preventDefault();
  const url = byId('cartURL').value.trim();
  byId('addLinkBtn').innerHTML = '<span class="spinner"></span> Extracting...';
  const added = await mutateCart({
    body: { email: activeEmail, url }, message: 'Product added to your cart.'
  });
  byId('addLinkBtn').textContent = 'Add to cart';
  if (added) {
    byId('cartURL').value = '';
    if (byId('productSearch').value.trim()) searchProducts();
  }
});

byId('productSearch').addEventListener('input', () => {
  searchController?.abort();
  clearTimeout(searchTimer);
  savedProducts = [];
  byId('searchResults').replaceChildren();
  byId('searchStatus').classList.remove('error-text');
  byId('searchStatus').textContent = byId('productSearch').value.trim() ? 'Searching saved products...' : SEARCH_HELP;
  searchTimer = setTimeout(searchProducts, 250);
});

byId('searchResults').addEventListener('click', event => {
  const button = event.target.closest('button[data-saved-product]');
  if (!button) return;
  mutateCart({
    body: { email: activeEmail, product_id: button.dataset.savedProduct }, message: 'Saved product added to your cart.'
  });
});

byId('cartItems').addEventListener('click', event => {
  const button = event.target.closest('button[data-action]');
  if (!button) return;
  const id = button.closest('[data-product-id]').dataset.productId;
  const item = cartItems.find(item => item.id === id);
  if (!item) return;
  if (button.dataset.action === 'remove') {
    mutateCart({
      path: `/cart/items/${encodeURIComponent(id)}?${emailQuery()}`, method: 'DELETE', removeID: id,
      message: 'Removed from cart. You can still find it in saved products.'
    });
  } else {
    updateQuantity(id, item.quantity + (button.dataset.action === 'increase' ? 1 : -1));
  }
});

byId('cartItems').addEventListener('change', event => {
  if (!event.target.matches('input[data-action="quantity"]')) return;
  updateQuantity(event.target.closest('[data-product-id]').dataset.productId, Number(event.target.value));
});

byId('cartItems').addEventListener('keydown', event => {
  if (event.key === 'Enter' && event.target.matches('input[data-action="quantity"]')) {
    event.preventDefault();
    event.target.blur();
  }
});

byId('shareCartBtn').addEventListener('click', openShare);
byId('exportTextBtn').addEventListener('click', exportText);
byId('dialogTextBtn').addEventListener('click', exportText);
byId('closeShareBtn').addEventListener('click', () => byId('shareDialog').close());
byId('shareDialog').addEventListener('close', resetPreparedImage);
byId('downloadImageBtn').addEventListener('click', () => {
  if (preparedImage) downloadBlob(preparedImage, exportFilename('png'));
});
byId('nativeShareBtn').addEventListener('click', async () => {
  if (!preparedFile) return;
  byId('nativeShareBtn').disabled = true;
  try {
    await navigator.share({ files: [preparedFile], title: 'Shopping list', text: 'Generated by Wishlist Tracker' });
  } catch (error) {
    if (error.name !== 'AbortError') toast(`Could not open sharing: ${error.message}. Use Download PNG instead.`, 'error');
  } finally {
    byId('nativeShareBtn').disabled = !preparedFile;
  }
});

const savedCartEmail = localStorage.getItem('wt_email') || '';
byId('cartEmail').value = savedCartEmail;
if (savedCartEmail) loadCart(savedCartEmail);
