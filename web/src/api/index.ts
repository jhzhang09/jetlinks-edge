import http from './http'

// ===== Auth =====
export interface LoginReq { username: string; password: string }
export interface LoginResp { token: string; user: { id: string; username: string; role: string }; ttlSec: number }

export function login(req: LoginReq) {
  return http.post<LoginResp, LoginResp>('/auth/login', req).then((r: any) => r.data as LoginResp)
}

export function me() {
  return http.get('/auth/me').then((r: any) => r.data)
}

export function changePassword(req: { oldPassword: string; newPassword: string }) {
  return http.post('/auth/password', req).then((r: any) => r.data)
}

// ===== Drivers / North =====
export function listDrivers() {
  return http.get('/drivers').then((r: any) => r.data as { items: string[] })
}

export type ConfigFieldType = 'text' | 'password' | 'number' | 'boolean' | 'select' | 'textarea'
export interface ConfigOption { label: string; value: any }
export interface ConfigField {
  key: string
  label: string
  type: ConfigFieldType
  required?: boolean
  defaultValue?: any
  placeholder?: string
  description?: string
  min?: number
  max?: number
  step?: number
  options?: ConfigOption[]
}
export interface ExtensionDescriptor {
  type: string
  name: string
  description?: string
  version: string
  capabilities?: string[]
  connectionSchema?: ConfigField[]
  tagSchema?: ConfigField[]
  configSchema?: ConfigField[]
}
export function listDriverExtensions() {
  return http.get('/extensions/drivers').then((r: any) => r.data as { items: ExtensionDescriptor[] })
}
export function listNorthExtensions() {
  return http.get('/extensions/north-apps').then((r: any) => r.data as { items: ExtensionDescriptor[] })
}
export function configDefaults(schema: ConfigField[] = [], current: Record<string, any> = {}) {
  const result = { ...current }
  for (const field of schema) {
    if (result[field.key] === undefined && field.defaultValue !== undefined) result[field.key] = field.defaultValue
  }
  return result
}

// ===== North Apps =====
export interface NorthApp {
  id: string
  name: string
  description?: string
  type: string
  enabled: boolean
  config: Record<string, any>
  createdAt?: string
  updatedAt?: string
}

export interface NorthAppStatus {
  id: string
  name: string
  type: string
  enabled: boolean
  running: boolean
  connected: boolean
  lastError?: string
}

export function listNorthApps() {
  return http.get('/north-apps').then((r: any) => r.data as { items: NorthAppStatus[]; types: string[] })
}
export function getNorthApp(id: string) {
  return http.get(`/north-apps/${id}`).then((r: any) => r.data as NorthApp)
}
export function createNorthApp(n: Partial<NorthApp>) {
  return http.post('/north-apps', n).then((r: any) => r.data as NorthApp)
}
export function updateNorthApp(id: string, n: Partial<NorthApp>) {
  return http.put(`/north-apps/${id}`, { ...n, id }).then((r: any) => r.data as NorthApp)
}
export function deleteNorthApp(id: string) {
  return http.delete(`/north-apps/${id}`).then((r: any) => r.data)
}
export function reloadNorthApp(id: string) {
  return http.post(`/north-apps/${id}/reload`).then((r: any) => r.data)
}

// ===== Connections =====
export interface Connection {
  id: string
  name: string
  description?: string
  driver: string
  enabled: boolean
  config: Record<string, any>
  createdAt?: string
  updatedAt?: string
}

export function listConnections() {
  return http.get('/connections').then((r: any) => r.data as { items: Connection[] })
}
export function getConnection(id: string) {
  return http.get(`/connections/${id}`).then((r: any) => r.data as Connection)
}
export function createConnection(c: Partial<Connection>) {
  return http.post('/connections', c).then((r: any) => r.data as Connection)
}
export function updateConnection(id: string, c: Partial<Connection>) {
  return http.put(`/connections/${id}`, { ...c, id }).then((r: any) => r.data as Connection)
}
export function deleteConnection(id: string) {
  return http.delete(`/connections/${id}`).then((r: any) => r.data)
}
export function listConnectionDrivers() {
  return http.get('/connections/drivers').then((r: any) => r.data as { items: string[] })
}

// ===== Groups =====
export interface DeviceConfig {
  productId: string
  deviceId: string
  secureKey?: string
}

export interface Group {
  id: string
  name: string
  description?: string
  connectionId: string
  driver?: string
  intervalMs: number
  config: Record<string, any>
  enabled: boolean
  northAppId?: string
  device: DeviceConfig
  tags?: Tag[]
}

export function listGroups() {
  return http.get('/groups').then((r: any) => r.data as { items: Group[] })
}
export function getGroup(id: string) {
  return http.get(`/groups/${id}`).then((r: any) => r.data as Group)
}
export function createGroup(g: Partial<Group>) {
  return http.post('/groups', g).then((r: any) => r.data as Group)
}
export function updateGroup(id: string, g: Partial<Group>) {
  return http.put(`/groups/${id}`, { ...g, id }).then((r: any) => r.data as Group)
}
export function deleteGroup(id: string) {
  return http.delete(`/groups/${id}`).then((r: any) => r.data)
}
export function reloadGroup(id: string) {
  return http.post(`/groups/${id}/reload`).then((r: any) => r.data)
}

// ===== Tags =====
export interface Tag {
  id: string
  groupId: string
  name: string
  address: string
  type: string
  byteOrder: string
  bit: number
  decimal: number
  precision: number
  access: 'ro' | 'wo' | 'rw'
  description?: string
  config: Record<string, any>
}

export function listTags(groupId: string) {
  return http.get(`/groups/${groupId}/tags`).then((r: any) => r.data as { items: Tag[] })
}
export function createTag(groupId: string, t: Partial<Tag>) {
  return http.post(`/groups/${groupId}/tags`, t).then((r: any) => r.data as Tag)
}
export function updateTag(id: string, t: Partial<Tag>) {
  return http.put(`/tags/${id}`, { ...t, id }).then((r: any) => r.data as Tag)
}
export function deleteTag(id: string) {
  return http.delete(`/tags/${id}`).then((r: any) => r.data)
}
export function readTag(id: string) {
  return http.post(`/tags/${id}/read`).then((r: any) => r.data)
}
export function writeTag(id: string, value: any) {
  return http.post(`/tags/${id}/write`, { value }).then((r: any) => r.data)
}
export function lastValues(groupId: string) {
  return http.get(`/groups/${groupId}/values`).then((r: any) => r.data as Record<string, { value: any; quality: string; time: string }>)
}

// ===== Status =====
export function status() {
  return http.get('/status').then((r: any) => r.data)
}

export interface OperationGroup {
  id: string
  name: string
  driver: string
  enabled: boolean
  connectionId: string
  northAppId?: string
  intervalMs: number
  running: boolean
  connected: boolean
  lastError?: string
  lastTime?: string
  stats?: Record<string, number>
  valueCount: number
  deviceId?: string
  description?: string
}

export interface OperationAlarm {
  id: string
  sourceType: 'group' | 'north' | 'tag'
  sourceId: string
  sourceName: string
  severity: 'critical' | 'warning'
  message: string
  time?: string
  route: string
}

export interface OperationValue {
  groupId: string
  groupName: string
  tagId: string
  name: string
  value: any
  quality: 'good' | 'bad' | 'uncertain'
  time: string
  error?: string
}

export interface OperationRuntime {
  nodeId: string
  goroutines: number
  memoryAllocBytes: number
  memorySysBytes: number
  memoryUsedPercent: number
  uptimeSeconds: number
}

export interface OperationConnection {
  id: string
  name: string
  driver: string
  enabled: boolean
  running: boolean
  connected: boolean
  lastError?: string
  lastTime?: string
}

export interface OperationsView {
  generatedAt: string
  startTime: string
  runtime: OperationRuntime
  connections: OperationConnection[]
  groups: OperationGroup[]
  northApps: NorthAppStatus[]
  driverPlugins: ExtensionDescriptor[]
  northPlugins: ExtensionDescriptor[]
  alarms: OperationAlarm[]
  recentValues: OperationValue[]
}

export function operations() {
  return http.get('/operations').then((r: any) => r.data as OperationsView)
}

export function browseOPCUA(groupId: string, nodeId?: string) {
  return http.get(`/groups/${groupId}/opcua/browse`, { params: { nodeId } }).then((r: any) => r.data as { items: any[] })
}
