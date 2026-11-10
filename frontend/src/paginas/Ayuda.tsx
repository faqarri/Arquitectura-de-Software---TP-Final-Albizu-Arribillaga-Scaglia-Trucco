import { Link } from 'react-router-dom'
import { useTitulo } from '../hooks/hooks'

const preguntas: [string, string][] = [
  ['¿Cómo compro un viaje?', 'Buscá un destino, entrá a la oferta que te guste y agregala al carrito. Cuando tengas todo, andá al carrito y tocá "Comprar". Te mandamos la factura por email.'],
  ['¿Por qué el carrito tiene una cuenta regresiva?', 'Al agregar una oferta te guardamos el lugar 15 minutos para que nadie más lo tome. Si no comprás en ese tiempo, la reserva vence y el lugar se libera. Podés volver a agregarlo si todavía hay cupo.'],
  ['¿Qué diferencia hay entre "Agotada" y "Sin cupo por ahora"?', '"Agotada" quiere decir que se vendieron todos los lugares. "Sin cupo por ahora" quiere decir que los lugares que quedan están reservados en carritos de otros viajeros: si no los compran, se liberan. Te mostramos a qué hora puede pasar.'],
  ['¿Qué es un pack?', 'Una promoción para un destino: elegís al menos un transporte, una estadía y una excursión, y obtenés un descuento sobre el total. Las fechas tienen que encajar: la estadía empieza después de llegar y las excursiones son durante la estadía.'],
  ['Mi compra dice "Procesando", ¿qué hago?', 'Nada: tu lugar sigue reservado y la compra se termina sola en unos segundos. Podés cerrar la pantalla; el resultado aparece en "Mis compras". Nunca se cobra dos veces.'],
  ['Mi compra fue rechazada, ¿me cobraron?', 'No. Si venció la reserva de algún ítem antes de confirmar, no se compra nada. Volvé al carrito, agregá de nuevo lo que venció y comprá otra vez.'],
  ['No me llegó la factura', 'Puede tardar unos minutos. Revisá la carpeta de spam. La factura se manda al email con el que ingresaste.'],
  ['¿Cómo comparto una guía de viaje?', 'Desde "Mis guías" o desde una compra confirmada. Solo podés compartir guías de destinos que compraste, en PDF de hasta 10 MB. La revisa un moderador antes de publicarla.'],
  ['Mi guía dice "Por enviar"', 'El servicio que publica las guías no respondió. La guardamos y la enviamos sola cuando vuelva; no hace falta que la subas de nuevo.'],
]

export function Ayuda() {
  useTitulo('Ayuda')
  return (
    <div className="pagina-angosta">
      <h1>Ayuda</h1>
      <p className="bajada">Las dudas más comunes. Tocá una pregunta para ver la respuesta.</p>
      <div className="preguntas">
        {preguntas.map(([p, r]) => (
          <details key={p} className="tarjeta">
            <summary>{p}</summary>
            <p>{r}</p>
          </details>
        ))}
      </div>
      <h2>Atajos de teclado</h2>
      <ul className="lista-simple">
        <li>
          <kbd>/</kbd> en la búsqueda: ir al campo de texto.
        </li>
        <li>
          <kbd>Esc</kbd>: cerrar un diálogo.
        </li>
        <li>
          <kbd>Tab</kbd>: recorrer la página; el primer enlace salta directo al contenido.
        </li>
      </ul>
      <p>
        <Link to="/" className="boton boton-primario">
          Volver a buscar
        </Link>
      </p>
    </div>
  )
}

export function NoEncontrada() {
  useTitulo('Página no encontrada')
  return (
    <div className="pagina-angosta centrado">
      <h1>No encontramos esta página</h1>
      <p>Puede que el enlace esté mal escrito o que la página ya no exista.</p>
      <Link to="/" className="boton boton-primario">
        Ir a buscar ofertas
      </Link>
    </div>
  )
}
