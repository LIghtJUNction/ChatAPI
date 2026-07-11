export type SettingLevel = 'common' | 'policy' | 'advanced' | 'startup'
export type SettingSource = 'default' | 'database' | 'environment'
export type SettingField = { key:string; type:string; title:string; description:string; level:SettingLevel; editable:boolean; sensitive:boolean; restart_required:boolean; default:unknown; minimum?:number; maximum?:number; enum?:string[]; unit?:string }
export type SettingsDocument = { domain:string; title:string; updated_at?:string; values:Record<string,unknown>; sources:Record<string,SettingSource>; fields:SettingField[]; stale?:boolean; refresh_error?:string }
export type SettingsPatchResult = { document:SettingsDocument; applied:string[]; restart_required:string[] }
