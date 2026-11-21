import { api } from './api.js';
import { esc } from './ui.js';

export async function loadProviderIndex() {
  const [providers, sites] = await Promise.all([api('/api/crm/providers'), api('/api/crm/sites')]);
  const clients = new Map();
  const modems = new Map();
  for (const site of sites) {
    if (site.archived) continue;
    if (site.client_id) {
      if (!clients.has(site.client_id)) clients.set(site.client_id, new Set());
      clients.get(site.client_id).add(site.provider_code);
    }
    for (const id of site.modem_ids) modems.set(id, site.provider_code);
  }
  return { providers, sites, clients, modems };
}

export function providerOptions(providers) {
  return '<option value="">Все провайдеры</option>' +
    providers.map(p => `<option value="${esc(p.code)}">${esc(p.name)}</option>`).join('');
}

export function providerBadges(index, codes) {
  return Array.from(codes || []).map(code => {
    const name = index.providers.find(p => p.code === code)?.name || code;
    return `<a class="provider-badge" href="#/sites?provider=${encodeURIComponent(code)}"><span aria-hidden="true">${esc(name[0])}</span>${esc(name)}</a>`;
  }).join(' ');
}
