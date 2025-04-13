"use strict";

import React from 'react';
class AbstractSource extends React.PureComponent {
  setNativeProps(props) {
    if (this._nativeRef) {
      this._nativeRef.setNativeProps(props);
    }
  }
  setNativeRef = instance => {
    this._nativeRef = instance;
  };
}
export default AbstractSource;
//# sourceMappingURL=AbstractSource.js.map