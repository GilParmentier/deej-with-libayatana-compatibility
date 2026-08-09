package deej

import (
	"fmt"
	"net"
	"strings"

	"github.com/jfreymuth/pulse/proto"
	"go.uber.org/zap"
)

// process.binary values that are generic wrappers/launchers rather than the
// actual running application - Wine and Proton report every game through one
// of these regardless of which .exe is actually running, so application.name
// (which they set to the real .exe name, e.g. "MTGA.exe") is a better source
var genericProcessBinaries = map[string]bool{
	"wine":             true,
	"wine64":           true,
	"wine-preloader":   true,
	"wine64-preloader": true,
	"wineserver":       true,
}

type paSessionFinder struct {
	logger        *zap.SugaredLogger
	sessionLogger *zap.SugaredLogger

	client *proto.Client
	conn   net.Conn
}

func newSessionFinder(logger *zap.SugaredLogger) (SessionFinder, error) {
	client, conn, err := proto.Connect("")
	if err != nil {
		logger.Warnw("Failed to establish PulseAudio connection", "error", err)
		return nil, fmt.Errorf("establish PulseAudio connection: %w", err)
	}

	request := proto.SetClientName{
		Props: proto.PropList{
			"application.name": proto.PropListString("deej"),
		},
	}
	reply := proto.SetClientNameReply{}

	if err := client.Request(&request, &reply); err != nil {
		return nil, err
	}

	sf := &paSessionFinder{
		logger:        logger.Named("session_finder"),
		sessionLogger: logger.Named("sessions"),
		client:        client,
		conn:          conn,
	}

	sf.logger.Debug("Created PA session finder instance")

	return sf, nil
}

func (sf *paSessionFinder) GetAllSessions() ([]Session, error) {
	sessions := []Session{}

	// get the master sink session
	masterSink, err := sf.getMasterSinkSession()
	if err == nil {
		sessions = append(sessions, masterSink)
	} else {
		sf.logger.Warnw("Failed to get master audio sink session", "error", err)
	}

	// get the master source session
	masterSource, err := sf.getMasterSourceSession()
	if err == nil {
		sessions = append(sessions, masterSource)
	} else {
		sf.logger.Warnw("Failed to get master audio source session", "error", err)
	}

	// enumerate sink inputs and add sessions along the way
	if err := sf.enumerateAndAddSessions(&sessions); err != nil {
		sf.logger.Warnw("Failed to enumerate audio sessions", "error", err)
		return nil, fmt.Errorf("enumerate audio sessions: %w", err)
	}

	return sessions, nil
}

func (sf *paSessionFinder) Release() error {
	if err := sf.conn.Close(); err != nil {
		sf.logger.Warnw("Failed to close PulseAudio connection", "error", err)
		return fmt.Errorf("close PulseAudio connection: %w", err)
	}

	sf.logger.Debug("Released PA session finder instance")

	return nil
}

func (sf *paSessionFinder) getMasterSinkSession() (Session, error) {
	request := proto.GetSinkInfo{
		SinkIndex: proto.Undefined,
	}
	reply := proto.GetSinkInfoReply{}

	if err := sf.client.Request(&request, &reply); err != nil {
		sf.logger.Warnw("Failed to get master sink info", "error", err)
		return nil, fmt.Errorf("get master sink info: %w", err)
	}

	// create the master sink session
	sink := newMasterSession(sf.sessionLogger, sf.client, reply.SinkIndex, reply.Channels, true)

	return sink, nil
}

func (sf *paSessionFinder) getMasterSourceSession() (Session, error) {
	request := proto.GetSourceInfo{
		SourceIndex: proto.Undefined,
	}
	reply := proto.GetSourceInfoReply{}

	if err := sf.client.Request(&request, &reply); err != nil {
		sf.logger.Warnw("Failed to get master source info", "error", err)
		return nil, fmt.Errorf("get master source info: %w", err)
	}

	// create the master source session
	source := newMasterSession(sf.sessionLogger, sf.client, reply.SourceIndex, reply.Channels, false)

	return source, nil
}

func (sf *paSessionFinder) enumerateAndAddSessions(sessions *[]Session) error {
	request := proto.GetSinkInputInfoList{}
	reply := proto.GetSinkInputInfoListReply{}

	if err := sf.client.Request(&request, &reply); err != nil {
		sf.logger.Warnw("Failed to get sink input list", "error", err)
		return fmt.Errorf("get sink input list: %w", err)
	}

	for _, info := range reply {
		name, matchedProperty, ok := resolveSinkInputName(info.Properties)

		if !ok {
			sf.logger.Warnw("Failed to get sink input's process name",
				"sinkInputIndex", info.SinkInputIndex,
				"availableProperties", propertyListToMap(info.Properties))

			continue
		}

		sf.logger.Debugw("Resolved sink input process name",
			"sinkInputIndex", info.SinkInputIndex,
			"matchedProperty", matchedProperty,
			"name", name)

		// create the deej session object
		newSession := newPASession(sf.sessionLogger, sf.client, info.SinkInputIndex, info.Channels, name)

		// add it to our slice
		*sessions = append(*sessions, newSession)

	}

	return nil
}

// resolveSinkInputName figures out the best process name to identify a sink
// input by. Some apps (e.g. sandboxed Flatpak/Snap builds of Spotify) don't
// set application.process.binary at all, and Wine/Proton games all report a
// generic launcher binary there instead of the actual .exe - in both cases we
// fall back to application.name (and then media.name) instead of either
// dropping the session or lumping every Wine game into one indistinguishable
// "wine64-preloader" session
func resolveSinkInputName(props proto.PropList) (string, string, bool) {
	binary, hasBinary := props["application.process.binary"]
	genericBinary := hasBinary && genericProcessBinaries[strings.ToLower(binary.String())]

	if hasBinary && !genericBinary {
		return binary.String(), "application.process.binary", true
	}

	if name, ok := props["application.name"]; ok {
		return name.String(), "application.name", true
	}

	if name, ok := props["media.name"]; ok {
		return name.String(), "media.name", true
	}

	// nothing more specific available - fall back to the generic binary name
	// rather than dropping the session entirely
	if hasBinary {
		return binary.String(), "application.process.binary", true
	}

	return "", "", false
}

// propertyListToMap converts a PulseAudio/PipeWire property list to a plain
// map of strings so it can be logged in a readable way for diagnostics
func propertyListToMap(props proto.PropList) map[string]string {
	result := make(map[string]string, len(props))

	for key, value := range props {
		result[key] = value.String()
	}

	return result
}
