"use strict";

Object.defineProperty(exports, "__esModule", {
  value: true
});
exports.default = void 0;
var _mapboxGl = require("mapbox-gl");
var _react = require("react");
var _reactDom = require("react-dom");
var _MapContext = _interopRequireDefault(require("../MapContext"));
function _interopRequireDefault(e) { return e && e.__esModule ? e : { default: e }; }
function MarkerView(props, ref) {
  const {
    map
  } = (0, _react.useContext)(_MapContext.default);

  // Create marker instance
  const marker = (0, _react.useMemo)(() => {
    const _marker = new _mapboxGl.Marker({
      element: /*#__PURE__*/(0, _react.isValidElement)(props.children) ? document.createElement('div') : undefined
    });

    // Set marker coordinates
    _marker.setLngLat(props.coordinate);

    // Fix marker position
    const {
      style
    } = _marker.getElement();
    style.position = 'absolute';
    style.top = '0';
    style.left = '0';
    return _marker;
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  // Add marker to map
  (0, _react.useEffect)(() => {
    if (map === undefined) {
      return;
    }
    marker.addTo(map);
    return () => {
      marker.remove();
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [map]);

  // Expose marker instance
  // eslint-disable-next-line react-hooks/exhaustive-deps
  (0, _react.useImperativeHandle)(ref, () => marker, []);

  // Update marker coordinates
  const markerCoordinate = marker.getLngLat();
  if (markerCoordinate.lng !== props.coordinate[0] || markerCoordinate.lat !== props.coordinate[1]) {
    marker.setLngLat([props.coordinate[0], props.coordinate[1]]);
  }

  // Inject children into marker element
  return /*#__PURE__*/(0, _reactDom.createPortal)(props.children, marker.getElement());
}
var _default = exports.default = /*#__PURE__*/(0, _react.memo)(/*#__PURE__*/(0, _react.forwardRef)(MarkerView));
//# sourceMappingURL=MarkerView.js.map