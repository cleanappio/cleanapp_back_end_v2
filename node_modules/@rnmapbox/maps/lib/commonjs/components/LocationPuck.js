"use strict";

Object.defineProperty(exports, "__esModule", {
  value: true
});
exports.default = void 0;
var _react = _interopRequireWildcard(require("react"));
var _reactNative = require("react-native");
var _RNMBXNativeUserLocationNativeComponent = _interopRequireDefault(require("../specs/RNMBXNativeUserLocationNativeComponent"));
var _jsxRuntime = require("react/jsx-runtime");
function _interopRequireDefault(e) { return e && e.__esModule ? e : { default: e }; }
function _getRequireWildcardCache(e) { if ("function" != typeof WeakMap) return null; var r = new WeakMap(), t = new WeakMap(); return (_getRequireWildcardCache = function (e) { return e ? t : r; })(e); }
function _interopRequireWildcard(e, r) { if (!r && e && e.__esModule) return e; if (null === e || "object" != typeof e && "function" != typeof e) return { default: e }; var t = _getRequireWildcardCache(r); if (t && t.has(e)) return t.get(e); var n = { __proto__: null }, a = Object.defineProperty && Object.getOwnPropertyDescriptor; for (var u in e) if ("default" !== u && {}.hasOwnProperty.call(e, u)) { var i = a ? Object.getOwnPropertyDescriptor(e, u) : null; i && (i.get || i.set) ? Object.defineProperty(n, u, i) : n[u] = e[u]; } return n.default = e, t && t.set(e, n), n; }
const defaultProps = {
  visible: true
};

/**
 * Renders a puck on the map that shows the device's current location.
 */
const LocationPuck = /*#__PURE__*/(0, _react.memo)(props => {
  const {
    iosShowsUserHeadingIndicator,
    pulsing,
    ...rest
  } = props;
  const nativePulsing = pulsing ? _pulsingToNative(pulsing) : undefined;
  let baseProps = {
    ...defaultProps,
    pulsing: nativePulsing
  };
  if (iosShowsUserHeadingIndicator) {
    console.warn('LocationPuck: iosShowsUserHeadingIndicator is deprecated, use puckBearingEnabled={true} puckBearing="heading" instead');
    baseProps = {
      ...baseProps,
      puckBearingEnabled: true,
      puckBearing: 'heading'
    };
  }
  const actualProps = {
    ...baseProps,
    ...rest
  };
  return /*#__PURE__*/(0, _jsxRuntime.jsx)(_RNMBXNativeUserLocationNativeComponent.default, {
    ...actualProps
  });
});
function _pulsingToNative(pulsing) {
  if (pulsing === 'default') {
    return {
      kind: 'default'
    };
  }
  if (pulsing == null) {
    return undefined;
  }
  const {
    color,
    isEnabled,
    radius
  } = pulsing;
  return {
    color: (0, _reactNative.processColor)(color),
    isEnabled,
    radius
  };
}
var _default = exports.default = LocationPuck;
//# sourceMappingURL=LocationPuck.js.map