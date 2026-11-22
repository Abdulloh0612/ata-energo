// Доменные типы CRM (вынесены из core.tsx).
export interface Me {
  id: number;
  username: string;
  role: string;
  role_display: string;
  first_name: string;
  last_name: string;
  client_id: number | null;
  client?: { name: string } | null;
}
export interface Grant {
  view: boolean;
  create: boolean;
  edit: boolean;
  assign: boolean;
  delete: boolean;
  own: boolean;
}
export interface Access {
  requests: Grant;
  tasks: Grant;
  sections: Record<string, boolean>;
}
export interface Device {
  id: number;
  name: string;
  client: number;
  client_name: string;
  modem: number | null;
  modem_serial: string | null;
  device_id: string;
  device_type: string;
  type_display: string;
  serial_number: string;
  status: string;
  last_seen: string | null;
  created_at: string;
  last_data: Record<string, unknown>;
}
export interface Site {
  id: string;
  name: string;
  status: string;
  client_id: number | null;
  client_name: string | null;
  latitude: number | null;
  longitude: number | null;
  address: string;
  timezone: string;
  provider_code: string;
  provider_name: string;
  modem_ids: number[];
  archived: boolean;
}
export interface AlertItem {
  id: number;
  device: number;
  device_code: string;
  device_type: string;
  client_id: number;
  client_name: string;
  kind: string;
  text: string;
  created_at: string;
  is_read: boolean;
}
