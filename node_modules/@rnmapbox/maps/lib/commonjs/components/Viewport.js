"use strict";

Object.defineProperty(exports, "__esModule", {
  value: true
});
exports.Viewport = void 0;
var _react = _interopRequireWildcard(require("react"));
var _RNMBXViewportNativeComponent = _interopRequireDefault(require("../specs/RNMBXViewportNativeComponent"));
var _NativeRNMBXViewportModule = _interopRequireDefault(require("../specs/NativeRNMBXViewportModule"));
var _NativeCommands = require("../utils/NativeCommands");
var _jsxRuntime = require("react/jsx-runtime");
function _interopRequireDefault(e) { return e && e.__esModule ? e : { default: e }; }
function _getRequireWildcardCache(e) { if ("function" != typeof WeakMap) return null; var r = new WeakMap(), t = new WeakMap(); return (_getRequireWildcardCache = function (e) { return e ? t : r; })(e); }
function _interopRequireWildcard(e, r) { if (!r && e && e.__esModule) return e; if (null === e || "object" != typeof e && "function" != typeof e) return { default: e }; var t = _getRequireWildcardCache(r); if (t && t.has(e)) return t.get(e); var n = { __proto__: null }, a = Object.defineProperty && Object.getOwnPropertyDescriptor; for (var u in e) if ("default" !== u && {}.hasOwnProperty.call(e, u)) { var i = a ? Object.getOwnPropertyDescriptor(e, u) : null; i && (i.get || i.set) ? Object.defineProperty(n, u, i) : n[u] = e[u]; } return n.default = e, t && t.set(e, n), n; }
/**
 * provides a structured approach to organizing camera management logic into states and transitions between them.
 *
 * At any given time, the viewport is either:
 *  - idle
 *  - in a state (camera is being managed by a ViewportState)
 *  - transitioning between states
 *
 * See [android](https://docs.mapbox.com/android/maps/api/${ANDROID_SDK_VERSION}/mapbox-maps-android/com.mapbox.maps.plugin.viewport/viewport.html),
 * [ios](https://docs.mapbox.com/ios/maps/api/${IOS_SDK_VERSION}/Viewport.html#/s:10MapboxMaps8ViewportC)
 */
const Viewport = exports.Viewport = /*#__PURE__*/(0, _react.memo)(/*#__PURE__*/(0, _react.forwardRef)((props, ref) => {
  const commands = (0, _react.useMemo)(() => new _NativeCommands.NativeCommands(_NativeRNMBXViewportModule.default), []);
  const nativeViewport = (0, _react.useRef)(null);
  (0, _react.useEffect)(() => {
    if (nativeViewport.current) {
      commands.setNativeRef(nativeViewport.current);
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [commands, nativeViewport.current]);
  (0, _react.useImperativeHandle)(ref, () => ({
    getState() {
      console.log(' => calling getState');
      return commands.call('getState', []);
    },
    async idle() {
      return commands.call('idle', []);
    },
    transitionTo(state, transition) {
      return commands.call('transitionTo', [state, transition]);
    }
  }));
  const onStatusChangedNative = (0, _react.useMemo)(() => {
    const propsOnStatusChanged = props.onStatusChanged;
    if (propsOnStatusChanged != null) {
      return event => {
        propsOnStatusChanged(event.nativeEvent.payload);
      };
    } else {
      return undefined;
    }
  }, [props.onStatusChanged]);
  return /*#__PURE__*/(0, _jsxRuntime.jsx)(RNMBXViewport, {
    ...props,
    hasStatusChanged: props.onStatusChanged != null,
    onStatusChanged: onStatusChangedNative,
    ref: nativeViewport
  });
}));
const RNMBXViewport = _RNMBXViewportNativeComponent.default;
//# sourceMappingURL=Viewport.js.map