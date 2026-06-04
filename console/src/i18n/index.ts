import i18n from 'i18next'
import { initReactI18next } from 'react-i18next'
import zhCN from './zh-CN.json'

// Day 1 ships zh-CN only. en-US + ja-JP bundles arrive when the first
// 跨境/海外 customer signs (see 财务模型 Y3 海外 5 ent assumption).
// Architecture is namespace-ready: add `import enUS from './en-US.json'`
// then resources.en = { translation: enUS }.

void i18n.use(initReactI18next).init({
  resources: {
    'zh-CN': { translation: zhCN },
  },
  lng: 'zh-CN',
  fallbackLng: 'zh-CN',
  interpolation: { escapeValue: false },
  returnNull: false,
})

export default i18n
