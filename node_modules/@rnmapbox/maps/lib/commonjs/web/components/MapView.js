"use strict";

Object.defineProperty(exports, "__esModule", {
  value: true
});
exports.default = void 0;
var _react = _interopRequireDefault(require("react"));
var _mapboxGl = _interopRequireDefault(require("mapbox-gl"));
var _MapContext = _interopRequireDefault(require("../MapContext"));
var _jsxRuntime = require("react/jsx-runtime");
function _interopRequireDefault(e) { return e && e.__esModule ? e : { default: e }; }
/**
 * MapView backed by Mapbox GL KS
 */class MapView extends _react.default.Component {
  state = {
    map: null
  };
  mapContainer = null;
  map = null;
  componentDidMount() {
    const {
      styleURL
    } = this.props;
    if (!this.mapContainer) {
      console.error('MapView - mapContainer should is null');
      return;
    }
    const map = new _mapboxGl.default.Map({
      container: this.mapContainer,
      style: styleURL || 'mapbox://styles/mapbox/streets-v11'
    });
    this.map = map;
    this.setState({
      map
    });
  }
  render() {
    const {
      children
    } = this.props;
    const {
      map
    } = this.state;
    return /*#__PURE__*/(0, _jsxRuntime.jsx)("div", {
      style: {
        width: '100%',
        height: '100%'
      },
      ref: el => {
        this.mapContainer = el;
      },
      children: map && /*#__PURE__*/(0, _jsxRuntime.jsx)("div", {
        style: {
          position: 'absolute'
        },
        children: /*#__PURE__*/(0, _jsxRuntime.jsx)(_MapContext.default.Provider, {
          value: {
            map
          },
          children: children
        })
      })
    });
  }
}
var _default = exports.default = MapView;
//# sourceMappingURL=MapView.js.map