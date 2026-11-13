'use strict';

const API = '';

function toast(message, type = 'success') {
  document.querySelectorAll('.toast').forEach(element => element.remove());
  const element = document.createElement('div');
  element.className = `toast ${type}`;
  element.setAttribute('role', type === 'error' ? 'alert' : 'status');
  element.textContent = message;
  document.body.appendChild(element);
  setTimeout(() => element.remove(), 4500);
}

function esc(value) {
  return String(value ?? '').replace(/[&<>"']/g, character => ({
    '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;'
  })[character]);
}

async function apiFetch(path, options) {
  const response = await fetch(`${API}${path}`, options);
  if (!response.ok) {
    const data = response.headers.get('content-type')?.includes('application/json')
      ? await response.json() : null;
    const message = data?.error || `Request failed (HTTP ${response.status})`;
    throw new Error(data?.details ? `${message}: ${data.details}` : message);
  }
  return response;
}

async function apiJSON(path, options) {
  const response = await apiFetch(path, options);
  return response.status === 204 ? null : response.json();
}
