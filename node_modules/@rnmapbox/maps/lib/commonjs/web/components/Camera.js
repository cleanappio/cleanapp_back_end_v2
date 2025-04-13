"use strict";

Object.defineProperty(exports, "__esModule", {
  value: true
});
exports.default = exports.Camera = void 0;
var _react = require("react");
var _MapContext = _interopRequireDefault(require("../MapContext"));
var _jsxRuntime = require("react/jsx-runtime");
function _interopRequireDefault(e) { return e && e.__esModule ? e : { default: e }; }
function isArray(value) {
  return value.length !== undefined;
}
function buildMapboxGlPadding(padding) {
  if (padding === undefined) {
    // undefined
    return undefined;
  } else if (!isArray(padding)) {
    // padding
    return padding;
  } else {
    // Array
    if (padding.length === 0) {
      // []
      return undefined;
    } else if (padding.length < 2) {
      // [padding]
      return padding[0];
    } else if (padding.length < 4) {
      // [vertical, horizontal]
      return {
        left: padding[0],
        right: padding[0],
        top: padding[1],
        bottom: padding[1]
      };
    } else {
      // [top, right, bottom, left]
      return {
        top: padding[0],
        right: padding[1],
        bottom: padding[2],
        left: padding[3]
      };
    }
  }
}
class Camera extends _react.Component {
  static contextType = _MapContext.default;
  static UserTrackingModes = [];
  componentDidMount() {
    const {
      map
    } = this.context;
    if (!map) {
      return;
    }

    // minZoomLevel
    if (this.props.minZoomLevel !== undefined) {
      map.setMinZoom(this.props.minZoomLevel);
    }

    // maxZoomLevel
    if (this.props.maxZoomLevel !== undefined) {
      map.setMaxZoom(this.props.maxZoomLevel);
    }

    // zoomLevel
    if (this.props.zoomLevel !== undefined) {
      map.setZoom(this.props.zoomLevel);
    }

    // centerCoordinate
    if (this.props.centerCoordinate !== undefined) {
      map.flyTo({
        center: this.props.centerCoordinate.slice(0, 2),
        duration: 0
      });
    }
  }
  fitBounds(northEastCoordinates, southWestCoordinates, padding = 0, animationDuration = 0) {
    const {
      map
    } = this.context;
    if (map) {
      map.fitBounds([northEastCoordinates.slice(0, 2), southWestCoordinates.slice(0, 2)], {
        padding: buildMapboxGlPadding(padding),
        duration: animationDuration
      });
    }
  }
  flyTo(centerCoordinate, animationDuration = 2000) {
    const {
      map
    } = this.context;
    if (map) {
      map.flyTo({
        center: centerCoordinate.slice(0, 2),
        duration: animationDuration
      });
    }
  }
  moveTo(centerCoordinate, animationDuration = 0) {
    const {
      map
    } = this.context;
    if (map) {
      map.easeTo({
        center: centerCoordinate.slice(0, 2),
        duration: animationDuration
      });
    }
  }
  zoomTo(zoomLevel, animationDuration = 2000) {
    const {
      map
    } = this.context;
    if (map) {
      map.flyTo({
        zoom: zoomLevel,
        duration: animationDuration
      });
    }
  }
  setCamera(props) {
    const {
      map
    } = this.context;
    if (!map) {
      return;
    }
    const {
      centerCoordinate,
      bounds,
      zoomLevel,
      heading,
      pitch,
      padding,
      animationDuration = 2000
    } = props;
    let options = {
      center: centerCoordinate?.slice(0, 2),
      zoom: zoomLevel ?? map.getZoom(),
      bearing: heading ?? map.getBearing(),
      pitch: pitch ?? map.getPitch()
    };
    if (padding?.paddingTop && padding?.paddingRight && padding?.paddingBottom && padding?.paddingLeft) {
      options.padding = buildMapboxGlPadding([padding.paddingTop, padding.paddingRight, padding.paddingBottom, padding.paddingLeft]);
    }
    if (bounds?.ne && bounds?.sw) {
      const newCameraTransform = map.cameraForBounds([bounds.ne, bounds.sw], options);
      options = {
        ...options,
        ...newCameraTransform
      };
    }
    switch (props.animationMode) {
      default:
      case 'easeTo':
      case 'linearTo':
        map.easeTo({
          ...options,
          duration: animationDuration
        });
        break;
      case 'flyTo':
        map.flyTo({
          ...options,
          duration: animationDuration
        });
        break;
      case 'moveTo':
      case 'none':
        map.jumpTo(options);
        break;
    }
  }
  render() {
    return /*#__PURE__*/(0, _jsxRuntime.jsx)(_jsxRuntime.Fragment, {});
  }
}
exports.Camera = Camera;
var _default = exports.default = Camera;
//# sourceMappingURL=Camera.js.map