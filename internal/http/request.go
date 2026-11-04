package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// maxBody is the largest request body the API reads.
const maxBody = 1 << 20

// decodeBody reads the JSON request body into dst. The body is limited to
// maxBody bytes (413 beyond that), holds exactly one JSON value and may not
// name a field dst does not have (400 with code invalid). With optional, an
// empty body leaves dst as it is.
func decodeBody(w http.ResponseWriter, r *http.Request, dst any, optional bool) error {
	body := http.MaxBytesReader(w, r.Body, maxBody)
	dec := json.NewDecoder(body)
	dec.DisallowUnknownFields()

	var tooBig *http.MaxBytesError
	tooLarge := &requestError{http.StatusRequestEntityTooLarge, "too_large", fmt.Sprintf("request body is larger than %d bytes", maxBody)}

	err := dec.Decode(dst)
	if errors.Is(err, io.EOF) && optional {
		return nil
	}
	if err == nil {
		// Nothing but white space may follow the value.
		_, err = dec.Token()
		switch {
		case errors.Is(err, io.EOF):
			return nil
		case errors.As(err, &tooBig):
			return tooLarge
		}
		return malformedRequest("request body must hold one JSON value")
	}

	var (
		syntax *json.SyntaxError
		typ    *json.UnmarshalTypeError
	)
	switch {
	case errors.As(err, &tooBig):
		return tooLarge
	case errors.Is(err, io.EOF):
		return malformedRequest("request body is empty")
	case errors.As(err, &syntax), errors.Is(err, io.ErrUnexpectedEOF):
		return malformedRequest("request body is not valid JSON")
	case errors.As(err, &typ):
		return malformedRequest(fmt.Sprintf("field %q has the wrong type", typ.Field))
	case strings.HasPrefix(err.Error(), "json: unknown field "):
		return malformedRequest(strings.TrimPrefix(err.Error(), "json: "))
	}
	return malformedRequest("request body is not valid")
}
