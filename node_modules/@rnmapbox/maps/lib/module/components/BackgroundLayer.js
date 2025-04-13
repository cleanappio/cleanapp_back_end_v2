"use strict";

import React from 'react';
import { NativeModules } from 'react-native';
import RNMBXBackgroundLayerNativeComponent from '../specs/RNMBXBackgroundLayerNativeComponent';
import AbstractLayer from './AbstractLayer';
import { jsx as _jsx } from "react/jsx-runtime";
const MapboxGL = NativeModules.RNMBXModule;
class BackgroundLayer extends AbstractLayer {
  static defaultProps = {
    sourceID: MapboxGL.StyleSource.DefaultSourceID
  };
  render() {
    const props = {
      ...this.baseProps,
      sourceLayerID: this.props.sourceLayerID
    };
    return /*#__PURE__*/_jsx(RNMBXBackgroundLayerNativeComponent
    // @ts-expect-error just codegen stuff
    , {
      ref: this.setNativeLayer,
      ...props
    });
  }
}
export default BackgroundLayer;
//# sourceMappingURL=BackgroundLayer.js.map