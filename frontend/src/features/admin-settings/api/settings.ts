import { requestJson } from '../../../lib/api'
import type { SettingsDocument, SettingsPatchResult } from '../model/types'
export async function getOverview(){return (await requestJson<{ok:boolean;overview:Record<string,unknown>}>('/api/admin/settings/overview')).overview}
export async function getRuntime(){return (await requestJson<{ok:boolean;runtime:Record<string,unknown>}>('/api/admin/settings/runtime')).runtime}
export async function getSettings(domain:string){return (await requestJson<{ok:boolean;document:SettingsDocument}>(`/api/admin/settings/${domain}`)).document}
export async function patchSettings(domain:string,values:Record<string,unknown>){return (await requestJson<{ok:boolean;result:SettingsPatchResult}>(`/api/admin/settings/${domain}`,{method:'PATCH',body:JSON.stringify({values})})).result}
export type AuditLogItem = { id:string; event_type:string; action:string; outcome:string; metadata?:Record<string, unknown>; created_at:string }
export async function getAuditLogs(eventType:string, limit=20){
  const params = new URLSearchParams({ event_type: eventType, limit: String(limit) })
  return (await requestJson<{ok:boolean;items:AuditLogItem[]}>(`/api/admin/audit/logs?${params}`)).items
}
