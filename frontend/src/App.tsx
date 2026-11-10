import { Route, Routes } from 'react-router-dom'
import { ProveedorCarrito } from './carrito/Carrito'
import { ProveedorAvisos } from './componentes/Avisos'
import { ConSesion, Marco } from './componentes/Marco'
import { AdminOfertas, AdminPacks, EditarOferta, NuevoPack } from './paginas/admin/Admin'
import { Ayuda, NoEncontrada } from './paginas/Ayuda'
import { Buscar } from './paginas/Buscar'
import { CarritoPagina } from './paginas/CarritoPagina'
import { CompraPagina, MisCompras } from './paginas/Compras'
import { CompartirGuia, MisGuias } from './paginas/Guias'
import { Ingresar } from './paginas/Ingresar'
import { Oferta } from './paginas/Oferta'
import { Pack } from './paginas/Pack'
import { ProveedorSesion } from './sesion/Sesion'

export function App() {
  return (
    <ProveedorAvisos>
      <ProveedorSesion>
        <ProveedorCarrito>
          <Routes>
            <Route element={<Marco />}>
              <Route index element={<Buscar />} />
              <Route path="ingresar" element={<Ingresar />} />
              <Route path="ofertas/:id" element={<Oferta />} />
              <Route path="packs/:id" element={<Pack />} />
              <Route path="ayuda" element={<Ayuda />} />
              <Route path="carrito" element={<ConSesion rol="cliente"><CarritoPagina /></ConSesion>} />
              <Route path="compras" element={<ConSesion rol="cliente"><MisCompras /></ConSesion>} />
              <Route path="compras/:id" element={<ConSesion rol="cliente"><CompraPagina /></ConSesion>} />
              <Route path="guias" element={<ConSesion rol="cliente"><MisGuias /></ConSesion>} />
              <Route path="guias/nueva" element={<ConSesion rol="cliente"><CompartirGuia /></ConSesion>} />
              <Route path="admin" element={<ConSesion rol="admin"><AdminOfertas /></ConSesion>} />
              <Route path="admin/packs" element={<ConSesion rol="admin"><AdminPacks /></ConSesion>} />
              <Route path="admin/packs/nuevo" element={<ConSesion rol="admin"><NuevoPack /></ConSesion>} />
              <Route path="admin/ofertas/nueva" element={<ConSesion rol="admin"><EditarOferta /></ConSesion>} />
              <Route path="admin/ofertas/:id" element={<ConSesion rol="admin"><EditarOferta /></ConSesion>} />
              <Route path="*" element={<NoEncontrada />} />
            </Route>
          </Routes>
        </ProveedorCarrito>
      </ProveedorSesion>
    </ProveedorAvisos>
  )
}
