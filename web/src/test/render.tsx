import type { ReactElement } from 'react'
import { render } from '@testing-library/react'
import { App as AntdApp, ConfigProvider } from 'antd'
import zhCN from 'antd/locale/zh_CN'
import { MemoryRouter, Route, Routes } from 'react-router-dom'

/** 页面测试统一包裹：Ant Design 上下文 + 中文语言包 + 内存路由 */
export function renderPage(element: ReactElement, options: { route?: string; path?: string } = {}) {
  const { route = '/', path = '*' } = options
  return render(
    <ConfigProvider locale={zhCN}>
      <AntdApp>
        <MemoryRouter initialEntries={[route]}>
          <Routes>
            <Route path={path} element={element} />
          </Routes>
        </MemoryRouter>
      </AntdApp>
    </ConfigProvider>,
  )
}
