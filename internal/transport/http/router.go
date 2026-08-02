package http

import (
	"net/http"

	appmw "finance-api/internal/transport/http/middleware"

	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"
)

// NewRouter собирает маршрутизатор API.
//
// chi выбран как тонкая надстройка над net/http: даёт группировку маршрутов,
// цепочки middleware и корректный 405 Method Not Allowed. Хендлеры при этом
// остаются обычными http.HandlerFunc, и отказ от chi не потребовал бы
// переписывать обработчики.
func NewRouter(h *Handlers) http.Handler {
	r := chi.NewRouter()

	// Recoverer превращает панику в 500 вместо обрыва соединения и падения
	// всего процесса. RequestID добавляет идентификатор для сопоставления
	// записей в логах.
	r.Use(chimw.RequestID)
	r.Use(chimw.Recoverer)

	// Ошибки маршрутизации возвращаются в том же конверте, что и остальные:
	// клиенту не приходится разбирать два разных формата ошибок.
	r.NotFound(func(w http.ResponseWriter, r *http.Request) {
		h.writeError(w, r, http.StatusNotFound, codeNotFound, "requested resource does not exist")
	})
	r.MethodNotAllowed(func(w http.ResponseWriter, r *http.Request) {
		h.writeError(w, r, http.StatusMethodNotAllowed, codeMethodNotAllowed,
			"method is not allowed for this resource")
	})

	r.Get("/healthz", h.health)

	r.Route("/api/v1", func(api chi.Router) {
		// Все прикладные маршруты требуют идентификатор пользователя.
		api.Use(appmw.RequireUserID(h.writeError))

		api.Route("/categories", func(categories chi.Router) {
			categories.Post("/", h.createCategory)
			categories.Get("/", h.listCategories)
		})

		api.Route("/operations", func(operations chi.Router) {
			operations.Post("/", h.createOperation)
			operations.Get("/", h.listOperations)
		})

		api.Get("/stats", h.getStats)
	})

	return r
}
