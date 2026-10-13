/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import { useId, useState } from 'react'
import { useFormContext, useWatch } from 'react-hook-form'
import { useTranslation } from 'react-i18next'

import { PasswordInput } from '@/components/password-input'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'

import type { ChannelFormValues } from '../lib/channel-form'

type UpstreamSettingKey =
  | 'upstream_recharge_rate'
  | 'upstream_group_ratio'
  | 'upstream_user_id'
  | 'upstream_access_token'

function parseSettings(raw: string | undefined): Record<string, unknown> {
  try {
    const parsed: unknown = JSON.parse(raw || '{}')
    return parsed && typeof parsed === 'object'
      ? (parsed as Record<string, unknown>)
      : {}
  } catch {
    return {}
  }
}

function SettingInput(props: {
  label: string
  description: string
  value: unknown
  numeric?: boolean
  secret?: boolean
  onChange: (text: string) => void
}) {
  const id = useId()
  const external = props.value == null ? '' : String(props.value)
  const [text, setText] = useState(external)
  const [prevExternal, setPrevExternal] = useState(external)
  // 表单重新载入渠道时同步；输入 "0." 这类中间状态时数值没变，不覆盖用户输入
  if (external !== prevExternal) {
    setPrevExternal(external)
    const same = props.numeric
      ? Number(text || 0) === Number(external || 0)
      : text === external
    if (!same) setText(external)
  }
  const handleChange = (value: string) => {
    setText(value)
    props.onChange(value)
  }
  const InputComponent = props.secret ? PasswordInput : Input

  return (
    <div className='space-y-1.5'>
      <Label htmlFor={id}>{props.label}</Label>
      <InputComponent
        id={id}
        value={text}
        inputMode={props.numeric ? 'decimal' : undefined}
        autoComplete='off'
        onChange={(e) => handleChange(e.target.value)}
      />
      <p className='text-muted-foreground text-xs'>{props.description}</p>
    </div>
  )
}

/** 上游充值比例、分组倍率和账户凭据，直接存进渠道的 settings JSON */
export function UpstreamBillingSettings() {
  const { t } = useTranslation()
  const form = useFormContext<ChannelFormValues>()
  const settings = parseSettings(
    useWatch({ control: form.control, name: 'settings' })
  )

  const update = (key: UpstreamSettingKey, text: string, numeric: boolean) => {
    const next = parseSettings(form.getValues('settings'))
    const trimmed = text.trim()
    const value = numeric ? Number(trimmed) : trimmed
    if (trimmed === '' || (numeric && !Number.isFinite(value))) {
      delete next[key]
    } else {
      next[key] = value
    }
    form.setValue('settings', JSON.stringify(next), { shouldDirty: true })
  }

  return (
    <div className='space-y-3 px-4 py-3'>
      <div className='space-y-0.5'>
        <Label>{t('Upstream cost and balance')}</Label>
        <p className='text-muted-foreground text-sm'>
          {t(
            'Track what each request costs on the upstream site and your account balance there. Sites with an access token are reconciled against the upstream bill for the exact charge'
          )}
        </p>
      </div>
      <div className='grid gap-4 sm:grid-cols-2'>
        <SettingInput
          label={t('Recharge rate (CNY per upstream $1)')}
          description={t(
            'Leave empty to skip estimation. Reconciled costs are then shown in upstream dollars'
          )}
          value={settings.upstream_recharge_rate}
          numeric
          onChange={(text) => update('upstream_recharge_rate', text, true)}
        />
        <SettingInput
          label={t('Upstream group ratio')}
          description={t(
            'Group ratio of your key on the upstream site, defaults to 1'
          )}
          value={settings.upstream_group_ratio}
          numeric
          onChange={(text) => update('upstream_group_ratio', text, true)}
        />
        <SettingInput
          label={t('Upstream user ID')}
          description={t('Required by older new-api sites, optional otherwise')}
          value={settings.upstream_user_id}
          onChange={(text) => update('upstream_user_id', text, false)}
        />
        <SettingInput
          label={t('Upstream access token')}
          description={t(
            'For new-api / one-api sites, create an access token in your personal settings there with the Profile and Usage view permissions. Fill it in one channel per site, the other channels of that site share it. Sub2API sites need nothing here'
          )}
          value={settings.upstream_access_token}
          secret
          onChange={(text) => update('upstream_access_token', text, false)}
        />
      </div>
    </div>
  )
}
