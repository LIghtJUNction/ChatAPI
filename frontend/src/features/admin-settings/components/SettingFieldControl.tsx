import { Input, InputNumber, Select, Switch, Tag, Typography } from 'antd'
import type { SettingField, SettingSource } from '../model/types'

type Props={field:SettingField;source:SettingSource;value:unknown;disabled?:boolean;onChange:(value:unknown)=>void}
export function SettingFieldControl({field,source,value,disabled:formDisabled=false,onChange}:Props){
  const disabled=formDisabled||!field.editable||source==='environment'
  let control
  if(field.type==='boolean') control=<Switch checked={Boolean(value)} disabled={disabled} onChange={onChange}/>
  else if(field.type==='integer') control=<InputNumber value={typeof value==='number'?value:Number(value??0)} min={field.minimum} max={field.maximum} precision={0} disabled={disabled} addonAfter={field.unit==='bytes'?'bytes':field.unit==='ms'?'ms':undefined} onChange={(next)=>onChange(Number(next??0))}/>
  else if(field.enum?.length) control=<Select value={String(value??'')} disabled={disabled} options={field.enum.map(item=>({value:item,label:item}))} onChange={onChange}/>
  else control=<Input value={String(value??'')} disabled={disabled} placeholder={field.unit==='duration'?'例如 30s、5m、1h':''} onChange={(event)=>onChange(event.target.value)}/>
  return <div className="admin-setting-row"><div className="admin-setting-copy"><div className="admin-setting-title"><Typography.Text strong>{field.title}</Typography.Text>{source==='environment'?<Tag color="blue">环境变量</Tag>:source==='database'?<Tag>已覆盖</Tag>:null}{field.restart_required?<Tag color="orange">需重启</Tag>:null}</div><Typography.Text type="secondary">{field.description}</Typography.Text></div><div className="admin-setting-control">{control}</div></div>
}
