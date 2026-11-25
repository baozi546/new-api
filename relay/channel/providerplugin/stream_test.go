package providerplugin

import (
	"bufio"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/pkg/jsplugin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLineSourceDetectsSSEWhenContentTypeIsMissing(t *testing.T) {
	body := "\uFEFFevent: response.created\r\ndata: " + `{"type":"response.created"}` + "\r\n\r\nevent: response.completed\r\ndata: " + `{"type":"response.completed"}` + "\r\n\r\n"
	reader := &lineSource{reader: bufio.NewReader(strings.NewReader(body))}

	first, more, err := reader.next()
	require.NoError(t, err)
	require.True(t, more)
	assert.Equal(t, jsplugin.StreamEvent{Data: `{"type":"response.created"}`, Event: "response.created"}, first)

	second, more, err := reader.next()
	require.NoError(t, err)
	require.True(t, more)
	assert.Equal(t, jsplugin.StreamEvent{Data: `{"type":"response.completed"}`, Event: "response.completed"}, second)

	_, more, err = reader.next()
	require.NoError(t, err)
	assert.False(t, more)
}

func TestLineSourceKeepsJSONLinesAsIndividualEvents(t *testing.T) {
	body := `{"id":1}` + "\n" + `{"id":2}` + "\n"
	reader := &lineSource{reader: bufio.NewReader(strings.NewReader(body))}

	first, more, err := reader.next()
	require.NoError(t, err)
	require.True(t, more)
	assert.Equal(t, jsplugin.StreamEvent{Data: `{"id":1}`}, first)

	second, more, err := reader.next()
	require.NoError(t, err)
	require.True(t, more)
	assert.Equal(t, jsplugin.StreamEvent{Data: `{"id":2}`}, second)

	_, more, err = reader.next()
	require.NoError(t, err)
	assert.False(t, more)
}