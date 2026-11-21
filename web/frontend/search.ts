import { api } from '../static/js/api.js';
import { store } from '../static/js/store.js';

type Result = { label: string; type: string; href: string };

export function initializeSearch() {
  const button = document.getElementById('search-open') as HTMLButtonElement;
  const dialog = document.getElementById('global-search') as HTMLDialogElement;
  const input = dialog.querySelector('input')!;
  const results = dialog.querySelector<HTMLElement>('.search-results')!;
  let items: Result[] = [];
  let opened = 0;

  function draw() {
    const query = input.value.trim().toLocaleLowerCase('ru');
    const filtered = items.filter(item => !query || item.label.toLocaleLowerCase('ru').includes(query)).slice(0, 12);
    results.replaceChildren();
    for (const item of filtered) {
      const link = document.createElement('a');
      link.href = item.href;
      const name = document.createElement('strong');
      const type = document.createElement('small');
      name.textContent = item.label;
      type.textContent = item.type;
      link.append(name, type);
      results.append(link);
    }
    if (!filtered.length) results.textContent = query ? 'Ничего не найдено' : 'Введите название для поиска';
  }

  async function open() {
    if (!store.me?.can_view_all || dialog.open) return;
    const generation = ++opened;
    const currentUser = store.me.id;
    items = [];
    input.value = '';
    results.textContent = 'Загрузка…';
    dialog.showModal();
    input.focus();
    try {
      const [clients, requests, sites] = await Promise.all([api('/api/clients/'), store.crmAccess.requests?.view ? api('/api/crm/requests/') : [],api('/api/crm/sites')]);
      if (generation !== opened || store.me?.id !== currentUser || !dialog.open) return;
      items = [
        ...sites.filter((site: {archived:boolean}) => !site.archived).map((site:{id:string;name:string}) => ({label:site.name,type:'Объект',href:'#/sites/'+site.id})),
        ...clients.map((client: { id: number; name: string }) => ({ label: client.name, type: 'Клиент', href: '#/client/' + client.id })),
        ...[...store.devices.values()].map(device => ({ label: [device.name || device.device_id,device.serial_number].filter(Boolean).join(' · '), type: 'Оборудование', href: '#/equipment/' + device.id })),
        ...requests.map((row: { id: number; title: string }) => ({ label: row.title, type: 'Заявка', href: '#/requests/' + row.id })),
      ];
      draw();
    } catch {
      if (generation === opened && dialog.open) results.textContent = 'Не удалось загрузить данные поиска';
    }
  }

  button.addEventListener('click', open);
  input.addEventListener('input', draw);
  dialog.addEventListener('click', event => {
    if (event.target instanceof Element && event.target.closest('a')) dialog.close();
  });
  document.addEventListener('keydown', event => {
    if ((event.ctrlKey || event.metaKey) && event.key.toLowerCase() === 'k' && store.me?.can_view_all) {
      event.preventDefault();
      void open();
    }
    if (dialog.open && event.key === 'Enter' && document.activeElement === input) {
      const first = results.querySelector('a');
      if (first) { event.preventDefault(); first.click(); }
    }
  });
}
