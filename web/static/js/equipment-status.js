const bishkekHour = new Intl.DateTimeFormat('en-GB', { timeZone: 'Asia/Bishkek', hour: 'numeric', hourCycle: 'h23' });

export function equipmentCondition(device, profile) {
  if (device.status === 'online' && device.last_data?.error) return 'problem';
  if (profile?.maintenance) return 'maintenance';
  if (device.status !== 'online') return 'offline';
  const solar = device.device_type === 'panel' || device.device_type === 'inverter';
  const hour = solar ? Number(bishkekHour.format()) : 12;
  if (hour >= 10 && hour < 15 && profile?.nominal_watts && typeof device.last_data?.power === 'number'
      && device.last_data.power < profile.nominal_watts * profile.low_power_percent / 100) return 'low';
  return 'normal';
}
